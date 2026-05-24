package main

import (
	"fmt"
	"sync"
	"time"
)

type SSHTerminalConfig struct {
	Listen             string `json:"listen"`
	HostKeyFile        string `json:"host_key_file"`
	Password           string `json:"password,omitempty"`
	AuthorizedKeysFile string `json:"authorized_keys_file,omitempty"`
}

type Tunnel struct {
	ID            int
	Description   string
	Remote        []*Serial
	Terminal      Endpoint // primary terminal line (local serial or remote SSH client)
	SSHTerminal   *SSHTerminalConfig
	QuitRequest   chan byte
	MonitorChan   chan byte
	EscapeChar1   byte
	EscapeChar2   byte
	Debug         bool
	activeIndex   int
	activeMu      sync.Mutex
	terminalsMu   sync.RWMutex
	sshTerminal   Endpoint // at most one SSH client session
}

func (t *Tunnel) setActiveIndex(i int) {
	t.activeMu.Lock()
	t.activeIndex = i
	t.activeMu.Unlock()
}

func (t *Tunnel) getActiveIndex() int {
	t.activeMu.Lock()
	defer t.activeMu.Unlock()
	return t.activeIndex
}

func (t *Tunnel) activeRemote() *Serial {
	idx := t.getActiveIndex()
	if idx < 0 || idx >= len(t.Remote) {
		return nil
	}
	return t.Remote[idx]
}

func (t *Tunnel) allTerminals() []Endpoint {
	t.terminalsMu.RLock()
	defer t.terminalsMu.RUnlock()
	out := make([]Endpoint, 0, 2)
	if t.Terminal != nil {
		out = append(out, t.Terminal)
	}
	if t.sshTerminal != nil {
		out = append(out, t.sshTerminal)
	}
	return out
}

func (t *Tunnel) attachSSHTerminal(ep Endpoint) error {
	t.terminalsMu.Lock()
	defer t.terminalsMu.Unlock()
	if t.sshTerminal != nil {
		return fmt.Errorf("ssh terminal already connected")
	}
	t.sshTerminal = ep
	return nil
}

func (t *Tunnel) detachSSHTerminal(ep Endpoint) {
	t.terminalsMu.Lock()
	defer t.terminalsMu.Unlock()
	if t.sshTerminal == ep {
		t.sshTerminal = nil
	}
}

func (t *Tunnel) writeToTerminals(b byte) {
	for _, term := range t.allTerminals() {
		term.EndpointChannels()[CINPUT] <- b
	}
}

func (t *Tunnel) writeToTerminalsBytes(data []byte) {
	for _, b := range data {
		t.writeToTerminals(b)
	}
}

func (t *Tunnel) forwardRemoteToTerminalsIfActive(Remote Serial, monitor *CLI, bytes ...byte) {
		active := t.activeRemote()
		if active == nil || active.ID != Remote.ID {
			return
		}
		for _, b := range bytes {
			t.writeToTerminals(b)
		}
}

func TunnelManager(tunnel *Tunnel, monitor *CLI) {
	monitor.RemoteToMonitor = make(map[int]bool)
	if len(tunnel.Remote) > 0 {
		tunnel.setActiveIndex(0)
	}

	for _, Remote := range tunnel.Remote {
		go func(Remote Serial) {
			logger.Infof("%s manager is alive +++", Remote.Description)
			for {
//				logger.Infof("\x1b[32m[>>]\x1b[0m anonymous func loop")
				select {
				case data := <-Remote.Channels[COUTPUT]:
					chName := fmt.Sprintf("Remote%d->terminals", Remote.ID)
					if monitor.RemoteToMonitor[Remote.ID] {
						tunnel.tunnelRouteDebug(chName, data, false, true, "\x1b[32m[>>]\x1b[0m forward to monitor CLI")
						monitor.stdin <- data
						continue
					}

					tunnel.tunnelRouteDebug(chName, data, false, false, "\x1b[32m[>>]\x1b[0m forward to terminals")
					tunnel.forwardRemoteToTerminalsIfActive(Remote, monitor, data)
				case <-Remote.Channels[RTERMINATE]:
					logger.Infof("\x1b[32m[>>]\x1b[0m Stopping Remote %d routine", Remote.ID)
					return
				}
			}
		}(*Remote)
	}

	if tunnel.Terminal != nil {
		if tunnel.Terminal.EndpointKind() == EndpointSerial {
			go tunnel.runTerminalInputLoop(tunnel.Terminal, monitor)
		} else {
			go tunnel.runSSHClientTerminalLoop()
		}
	}

	<-tunnel.QuitRequest

	for _, Remote := range tunnel.Remote {
		Remote.Channels[CONTROL] <- 0
	}
	if tunnel.Terminal != nil {
		tunnel.Terminal.EndpointChannels()[CONTROL] <- 0
	}
	tunnel.terminalsMu.RLock()
	if tunnel.sshTerminal != nil {
		tunnel.sshTerminal.EndpointChannels()[CONTROL] <- 0
	}
	tunnel.terminalsMu.RUnlock()

	time.Sleep(time.Second)
	logger.Infof("tunnel %d stopped", tunnel.ID)
}

func (t *Tunnel) forwardToActiveRemote(b byte, monitor *CLI) {
	if ! monitor.Used { //normal forward to serial
		if active := t.activeRemote(); active != nil {
			active.Channels[CINPUT] <- b
		} else if t.Terminal != nil && t.Terminal.EndpointKind() == EndpointSSH {
			t.Terminal.EndpointChannels()[CINPUT] <- b
		}
	} else { // monitor mode forward to monitor cli
		monitor.stdin <- b
	}
}

func (t *Tunnel) runTerminalInputLoop(term Endpoint, monitor *CLI) {
	ch := term.EndpointChannels()
	var pendingConsoleEsc bool
	for {
//		logger.Infof("\x1b[31m[<<]\x1b[0m runTerminalInputLoop loop")
		select {
		case data := <-ch[COUTPUT]:
			chName := terminalChannelName(term)
			if pendingConsoleEsc {
				pendingConsoleEsc = false
				if idx, ok := RemoteIndexFromSelectChar(byte(data), len(t.Remote)); ok {
					t.tunnelRouteDebug(chName, data, true, true, fmt.Sprintf("\x1b[31m[<<]\x1b[0m select Remote index %d (digit not forwarded)", idx))
					t.setActiveIndex(idx)
					active := t.activeRemote()
					msg := fmt.Sprintf("\r\n *** serial_tunnel: console %d (%s) ***\r\n",
						idx, active.Description)
					t.writeToTerminalsBytes([]byte(msg))
					logger.Infof("\x1b[31m[<<]\x1b[0m tunnel %d: switched to Remote index %d", t.ID, idx)
					continue
				}
				t.tunnelRouteDebug(chName, data, true, false, "\x1b[31m[<<]\x1b[0m select mismatch, forward byte to Remote")
				t.forwardToActiveRemote(data, monitor)

				if byte(data) == t.EscapeChar2 {
					t.tunnelRouteDebug(chName, data, true, true, "\x1b[31m[<<]\x1b[0m monitor escape complete (esc2 not forwarded)")
					if !monitor.Used {
						logger.Infof("\x1b[31m[<<]\x1b[0m connected to monitor")
						msg := "\r\n *** serial_tunnel: console -1 (monitor) ***\r\n"
						t.writeToTerminalsBytes([]byte(msg))
						monitor.tconnected = t
						monitor.Used = true
					} else {
						logger.Warnf("\x1b[31m[<<]\x1b[0m requested monitor but monitor is in use")
					}
					continue
				}

				if byte(data) == t.EscapeChar1 {
					t.tunnelRouteDebug(chName, data, false, true, "\x1b[31m[<<]\x1b[0m esc1 forward+pending (after select mismatch)")
					pendingConsoleEsc = true
				}
				continue
			}
			if byte(data) == t.EscapeChar1 {
				t.tunnelRouteDebug(chName, data, false, true, "\x1b[31m[<<]\x1b[0m esc1 forward+pending (console select)")
				t.forwardToActiveRemote(data, monitor)
				pendingConsoleEsc = true
				continue
			}
			t.tunnelRouteDebug(chName, data, false, false, "\x1b[31m[<<]\x1b[0m forward to active Remote")
			t.forwardToActiveRemote(data, monitor)
		case <-ch[CONTROL]:
			return
		}
	}
}

func (t *Tunnel) runSSHClientTerminalLoop() {
	ch := t.Terminal.EndpointChannels()
	for {
		select {
		case data := <-ch[COUTPUT]:
			t.tunnelRouteDebug("SSH->Remotes", data, false, false, "forward to Remotes")
			t.writeToTerminals(data)
		case <-ch[CONTROL]:
			return
		}
	}
}

func terminalChannelName(term Endpoint) string {
	if term.EndpointDescription() == "ssh-terminal" {
		return "SSH-terminal->Remote"
	}
	switch term.EndpointKind() {
	case EndpointSSH:
		return "SSH->Remotes"
	default:
		return fmt.Sprintf("Terminal%d->Remotes", term.EndpointID())
	}
}

func RemoteIndexFromSelectChar(b byte, count int) (int, bool) {
	if b < '0' || b > '9' {
		return 0, false
	}
	idx := int(b - '0')
	if idx >= count {
		return 0, false
	}
	return idx, true
}

func RemoteFuncFromSelectChar(b byte) (int, bool) {
	switch (b) {
		case 'x':
		case 'm':
		default:
			return int(b), false
	}
	return int(b), true
}
