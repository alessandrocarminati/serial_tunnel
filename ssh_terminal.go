package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"time"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

type sshTerminalEndpoint struct {
	Channels [5]chan byte
	sshConn *ssh.ServerConn
}

func (s *sshTerminalEndpoint) EndpointID() int                   { return -1 }
func (s *sshTerminalEndpoint) EndpointDescription() string     { return "ssh-terminal" }
func (s *sshTerminalEndpoint) EndpointChannels() *[5]chan byte { return &s.Channels }
func (s *sshTerminalEndpoint) EndpointKind() EndpointKind      { return EndpointSerial }
func (s *sshTerminalEndpoint) EndpointConfig() string          {
	output := "Not connected"

	if s.sshConn != nil {
		output = fmt.Sprintf("Connected (%s)", s.sshConn.RemoteAddr())
	}
	return output
}

func startSSHTerminalServer(tunnel *Tunnel, cfg SSHTerminalConfig, monitor *CLI) {
	hostKey, err := loadOrCreateHostKey(cfg.HostKeyFile)
	if err != nil {
		logger.Errorf("tunnel %d ssh terminal: host key: %v", tunnel.ID, err)
		return
	}

	serverCfg := &ssh.ServerConfig{}
	if cfg.Password != "" {
		pw := cfg.Password
		serverCfg.PasswordCallback = func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == pw {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected")
		}
	}
	if cfg.AuthorizedKeysFile != "" {
		keys, err := loadAuthorizedKeys(cfg.AuthorizedKeysFile)
		if err != nil {
			logger.Errorf("tunnel %d ssh terminal: authorized_keys: %v", tunnel.ID, err)
			return
		}
		serverCfg.PublicKeyCallback = func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			for _, ak := range keys {
				if bytes.Equal(ak.Marshal(), key.Marshal()) {
					return nil, nil
				}
			}
			return nil, fmt.Errorf("public key rejected")
		}
	}
	if cfg.Password == "" && cfg.AuthorizedKeysFile == "" {
		logger.Errorf("tunnel %d ssh terminal: set password and/or authorized_keys_file", tunnel.ID)
		return
	}
	serverCfg.AddHostKey(hostKey)

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		logger.Errorf("tunnel %d ssh terminal: listen %s: %v", tunnel.ID, cfg.Listen, err)
		return
	}
	logger.Infof("tunnel %d ssh terminal listening on %s (single client)", tunnel.ID, cfg.Listen)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				logger.Errorf("tunnel %d ssh terminal accept: %v", tunnel.ID, err)
				return
			}
			go handleSSHTerminalConnection(tunnel, conn, serverCfg, monitor)
		}
	}()
}

func handleSSHTerminalConnection(tunnel *Tunnel, conn net.Conn, cfg *ssh.ServerConfig, monitor *CLI) {
	var ep *sshTerminalEndpoint
	defer conn.Close()

	// OS-level TCP keepalive.
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		logger.Warnf("tunnel %d ssh terminal handshake failed: %v", tunnel.ID, err)
		return
	}
	defer sshConn.Close()

	go ssh.DiscardRequests(reqs)

	logger.Infof("tunnel %d ssh terminal: client %s connected", tunnel.ID, sshConn.RemoteAddr())

	connDone := make(chan struct{})

	go func() {
		err := sshConn.Wait()
		logger.Infof("tunnel %d ssh terminal: client %s disconnected: %v",
			tunnel.ID, sshConn.RemoteAddr(), err)
		tunnel.detachSSHTerminal(ep)
		ep.EndpointChannels()[CONTROL] <- 0
		close(connDone)
	}()

	go monitorSSHKeepalive(tunnel.ID, sshConn, conn, connDone)

	for {
		select {
		case <-connDone:
			return

		case newChannel, ok := <-chans:
			if !ok {
				return
			}

			if newChannel.ChannelType() != "session" {
				_ = newChannel.Reject(ssh.UnknownChannelType, "only session channels supported")
				continue
			}

			channel, requests, err := newChannel.Accept()
			if err != nil {
				logger.Errorf("tunnel %d ssh terminal channel accept: %v", tunnel.ID, err)
				return
			}

			ep = &sshTerminalEndpoint{}
			initEndpointChannels(ep)
			ep.sshConn = sshConn

			if err := tunnel.attachSSHTerminal(ep); err != nil {
				_, _ = channel.Write([]byte(err.Error() + "\r\n"))
				_ = channel.Close()
				logger.Warnf("tunnel %d ssh terminal: %v", tunnel.ID, err)
				return
			}

			go tunnel.runTerminalInputLoop(ep, monitor)

			serveSSHTerminalSession(tunnel, ep, channel, requests)

			tunnel.detachSSHTerminal(ep)
			ep.EndpointChannels()[CONTROL] <- 0
			return
		}
	}
}

func monitorSSHKeepalive(tunnelID int, sshConn *ssh.ServerConn, rawConn net.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return

		case <-ticker.C:
			pingDone := make(chan error, 1)

			go func() {
				_, _, err := sshConn.SendRequest("keepalive@openssh.com", true, nil)
				pingDone <- err
			}()

			select {
			case <-done:
				return

			case err := <-pingDone:
				if err != nil {
					logger.Warnf("tunnel %d ssh terminal keepalive failed: %v", tunnelID, err)
					_ = sshConn.Close()
					_ = rawConn.Close()
					return
				}

			case <-time.After(10 * time.Second):
				logger.Warnf("tunnel %d ssh terminal keepalive timeout", tunnelID)
				_ = sshConn.Close()
				_ = rawConn.Close()
				return
			}
		}
	}
}

func serveSSHTerminalSession(tunnel *Tunnel, ep *sshTerminalEndpoint, channel ssh.Channel, requests <-chan *ssh.Request) {
	shellStarted := false
	for req := range requests {
		switch req.Type {
		case "pty-req":
			req.Reply(true, nil)
		case "shell":
			if shellStarted {
				req.Reply(false, nil)
				continue
			}
			shellStarted = true
			req.Reply(true, nil)
			runSSHTerminalBridge(tunnel, ep, channel)
			return
		case "exec":
			req.Reply(false, nil)
		default:
			req.Reply(false, nil)
		}
	}
}

func runSSHTerminalBridge(tunnel *Tunnel, ep *sshTerminalEndpoint, channel ssh.Channel) {
	defer channel.Close()

	go func() {
		buf := make([]byte, 1)
		for {
			n, err := channel.Read(buf)
			if n > 0 {
				ep.Channels[COUTPUT] <- buf[0]
				continue
			}
			if err != nil {
				if err != io.EOF {
					logger.Infof("tunnel %d ssh terminal read ended: %v", tunnel.ID, err)
				}
				ep.Channels[RTERMINATE] <- 0
				return
			}
		}
	}()

	for {
		select {
		case data := <-ep.Channels[CINPUT]:
			if _, err := channel.Write([]byte{data}); err != nil {
				logger.Infof("tunnel %d ssh terminal write ended: %v", tunnel.ID, err)
				return
			}
		case <-ep.Channels[STERMINATE]:
			return
		case <-ep.Channels[CONTROL]:
			return
		}
	}
}

func loadOrCreateHostKey(path string) (ssh.Signer, error) {
	if path == "" {
		path = "ssh_host_key"
	}
	if data, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(data)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	pemBytes, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pemBytes.Bytes, 0600); err != nil {
		logger.Warnf("could not write host key to %s: %v", path, err)
	}
	return ssh.NewSignerFromKey(priv)
}

func loadAuthorizedKeys(path string) ([]ssh.PublicKey, error) {
	data, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, err
	}
	var keys []ssh.PublicKey
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("authorized_keys %q: %w", line, err)
		}
		keys = append(keys, pub)
	}
	return keys, nil
}
