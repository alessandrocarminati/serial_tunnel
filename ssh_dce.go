package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHEndpoint struct {
	ID             int          `json:"id"`
	Description    string       `json:"description"`
	Channels       [5]chan byte `json:"-"`
	Host           string       `json:"host"`
	Port           int          `json:"port"`
	User           string       `json:"user"`
	Password       string       `json:"password,omitempty"`
	KeyFile        string       `json:"keyfile,omitempty"`
	RemoteCommand  string       `json:"remote_command"`
	ConnectTimeout int          `json:"connect_timeout,omitempty"` // seconds
}

func (s *SSHEndpoint) EndpointID() int                  { return s.ID }
func (s *SSHEndpoint) EndpointDescription() string    { return s.Description }
func (s *SSHEndpoint) EndpointChannels() *[5]chan byte { return &s.Channels }
func (s *SSHEndpoint) EndpointKind() EndpointKind      { return EndpointSSH }
func (s *SSHEndpoint) EndpointConfig() string      { return "dummy SSHEndpoint" }

func (s *SSHEndpoint) dialTimeout() time.Duration {
	if s.ConnectTimeout > 0 {
		return time.Duration(s.ConnectTimeout) * time.Second
	}
	return 1 * time.Second
}

func (s *SSHEndpoint) addr() string {
	port := s.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(s.Host, fmt.Sprintf("%d", port))
}

func expandHome(path string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
}

func (s *SSHEndpoint) clientConfig() (*ssh.ClientConfig, error) {
	var auths []ssh.AuthMethod

	if s.Password != "" {
		auths = append(auths, ssh.Password(s.Password))
	}
	if s.KeyFile != "" {
		keyPath := expandHome(s.KeyFile)
		keyBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read keyfile %q: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("parse keyfile %q: %w", keyPath, err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("ssh endpoint %d: no password or keyfile configured", s.ID)
	}

	user := s.User
	if user == "" {
		user = "root"
	}

	return &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // local/lab devices
		Timeout:         s.dialTimeout(),
	}, nil
}

func SSHManager(ep SSHEndpoint) {
	logger.Infof("starting SSH Manager on endpoint %d - %s@%s (%s)",
		ep.ID, ep.User, ep.addr(), ep.Description)

	cfg, err := ep.clientConfig()
	if err != nil {
		logger.Errorf("SSH endpoint %d config error: %v", ep.ID, err)
		return
	}

	client, err := ssh.Dial("tcp", ep.addr(), cfg)
	if err != nil {
		logger.Errorf("SSH endpoint %d dial error: %v", ep.ID, err)
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		logger.Errorf("SSH endpoint %d session error: %v", ep.ID, err)
		return
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		logger.Errorf("SSH endpoint %d stdin pipe error: %v", ep.ID, err)
		return
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		logger.Errorf("SSH endpoint %d stdout pipe error: %v", ep.ID, err)
		return
	}
	session.Stderr = session.Stdout

	if ep.RemoteCommand != "" {
		if err := session.Start(ep.RemoteCommand); err != nil {
			logger.Errorf("SSH endpoint %d start %q: %v", ep.ID, ep.RemoteCommand, err)
			return
		}
	} else {
		if err := session.Shell(); err != nil {
			logger.Errorf("SSH endpoint %d shell error: %v", ep.ID, err)
			return
		}
	}

	go sshReadLoop(ep, stdout)
	go sshWriteLoop(ep, stdin)

	<-ep.Channels[CONTROL]

	ep.Channels[RTERMINATE] <- 0
	ep.Channels[STERMINATE] <- 0
	time.Sleep(time.Second)
	logger.Infof("SSHManager for endpoint %d is stopped.", ep.ID)
}

func sshReadLoop(ep SSHEndpoint, r io.Reader) {
	buf := make([]byte, 1)
	for {
		select {
		case <-ep.Channels[RTERMINATE]:
			logger.Infof("Killing SSH %d receive thread", ep.ID)
			return
		default:
			n, err := r.Read(buf)
			if n > 0 {
				ep.Channels[COUTPUT] <- buf[0]
				continue
			}
			if err != nil {
				if err != io.EOF {
					logger.Errorf("SSH endpoint %d read error: %v", ep.ID, err)
				}
				return
			}
		}
	}
}

func sshWriteLoop(ep SSHEndpoint, w io.Writer) {
	for {
		select {
		case data := <-ep.Channels[CINPUT]:
			if _, err := w.Write([]byte{data}); err != nil {
				logger.Errorf("SSH endpoint %d write error: %v", ep.ID, err)
				return
			}
		case <-ep.Channels[STERMINATE]:
			logger.Infof("Killing SSH %d send thread", ep.ID)
			return
		}
	}
}

func startEndpointManager(e Endpoint) {
	switch e.EndpointKind() {
	case EndpointSerial:
		go SerialManager(*e.(*Serial))
	case EndpointSSH:
		go SSHManager(*e.(*SSHEndpoint))
	default:
		logger.Errorf("unknown endpoint kind for id %d", e.EndpointID())
	}
}
