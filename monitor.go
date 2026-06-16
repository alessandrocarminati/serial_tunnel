package main

import (
	"fmt"
	"strings"
	"sync"
)

type CommandFunction func(args []string, tunnel *Tunnel, m *CLI)

type Command struct {
	Name        string
	HelpText    string
	Handler     CommandFunction
}

type CLI struct {
	commands        map[string]Command
	stdin           chan byte
	stdout          *chan byte
	tconnected      *Tunnel
	quit            chan byte
	Used            bool
	registerMu      sync.Mutex
	ConnectedID     int
	RemoteToMonitor map[int]bool
}

func NewCLI(stdin chan byte, stdout *chan byte, quit chan byte) *CLI {
	return &CLI{
		commands: make(map[string]Command),
		stdin:    stdin,
		stdout:   stdout,
		quit:     quit,
		Used:     false,
	}
}

func (c *CLI) RegisterCommand(name, helpText string, handler CommandFunction) {
	c.registerMu.Lock()
	defer c.registerMu.Unlock()

	logger.Infof("New command registered in the CLI (%s)", name)
	c.commands[name] = Command{
		Name:     name,
		HelpText: helpText,
		Handler:  handler,
	}
}

func (c *CLI) ListCommands() string {
	c.registerMu.Lock()
	defer c.registerMu.Unlock()

	logger.Debugf("Help command issued in the CLI")
	var result strings.Builder
	result.WriteString("Available commands:\r\n")
	for _, cmd := range c.commands {
		result.WriteString(fmt.Sprintf("%-15s %s\r\n", cmd.Name, cmd.HelpText))
	}
	return result.String()
}

func (c *CLI) Run() {
	logger.Infof("CLI is alive.")
	go c.inputLoop()
}

func (c *CLI) inputLoop() {
	buffer := make([]byte, 0)

	for {
		select {
		case b := <-c.stdin:
			switch {
			case b == 13:
				input := string(buffer)
				logger.Infof("CLI received a the command \"%s\".", input)
				c.parseInput(input)
				buffer = nil // reset buffer
			case b == 8 || b == 127:
				if len(buffer) > 0 {
					buffer = buffer[:len(buffer)-1]
					c.tconnected.writeToTerminals(8)
					c.tconnected.writeToTerminals(32)
					c.tconnected.writeToTerminals(8)
				}
			case b > 31 && b < 127:
				buffer = append(buffer, b)
				logger.Tracef("CLI is receiving things: last char= %d current buffer\"%s\"", b, string(buffer))
				c.tconnected.writeToTerminals(b)
			default:
			}
		case <-c.quit:
			return
		}
	}
}

func (c *CLI) parseInput(input string) {
	args := strings.Fields(input)
	if len(args) == 0 {
		return
	}

	command := args[0]
	switch command {
	case "help":
		for _, b := range []byte(c.ListCommands()) {
			c.tconnected.writeToTerminals(b)
		}
	case "quit":
		c.quit <- 1
	default:
		c.registerMu.Lock()
		cmd, found := c.commands[command]
		c.registerMu.Unlock()

		if found {
			cmd.Handler(args[1:], c.tconnected, c)
		} else {
			for _, b := range []byte(fmt.Sprintf("\r\nUnknown command: %s\r\ntype 'help' to see available commands\r\n", command)) {
				c.tconnected.writeToTerminals(b)
			}
		}
	}
}

func testHandler(args []string, t *Tunnel, m *CLI) {
	logger.Infof("testHandler function executed.")
	output := fmt.Sprintf("Test command executed with args: %v\r\n", args)
	for _, b := range []byte(output) {
		t.writeToTerminals(b)
	}
}


func showHandler(args []string, t *Tunnel, m *CLI) {
	subcommands := []Command{
		{
			Name:     "remote",
			HelpText: "Show current Remotes",
			Handler:  showRemote,
		},
		{
			Name:     "terminal",
			HelpText: "Show ssh terminals state",
			Handler:  showTerminal,
		},
	}

	logger.Infof("showHandler function executed.")
	if len(args) == 0 || args[0] == "?" {
		help := "Available options for 'show':\r\n"
		for _, cmd := range subcommands {
			help += fmt.Sprintf("%-25s %s\r\n", cmd.Name, cmd.HelpText)
		}
		for _, b := range []byte(help) {
			t.writeToTerminals(b)
		}
		return
	}

	subcommandName := args[0]
	for _, cmd := range subcommands {
		if cmd.Name == subcommandName {
			cmd.Handler(args[1:], t, m)
			return
		}
	}

	logger.Debugf("Unknown \"show\" subcommand requested (%s).", args[0])
	for _, b := range []byte("Unknown option. Use 'show ?' for help.\r\n") {
	t.writeToTerminals(b)
	}
}

func showRemote(args []string, t *Tunnel, m *CLI) {
	logger.Infof("showRemote function executed.")
	output := fmt.Sprintf("Remotes summary (%d):\r\n", len(t.Remote))
	for i, Remote := range  t.Remote {
		line := fmt.Sprintf("%d: %s\r\n", i, Remote.EndpointConfig())
		output = output + line
	}
	for _, b := range []byte(output) {
		t.writeToTerminals(b)
	}
}

func showTerminal(args []string, t *Tunnel, m *CLI) {
	logger.Infof("showvterminal function executed.")

	output := fmt.Sprintf("Terminals state:\r\n")
	Terminal := fmt.Sprintf("SERIAL: %s\r\n", t.Terminal.EndpointConfig())
	vTerminal := "Not connected\r\n"
	if t.sshTerminal != nil {
		vTerminal = fmt.Sprintf("SSH: %s\r\n", t.sshTerminal.EndpointConfig())
	}
	output = output + Terminal + vTerminal

	for _, b := range []byte(output) {
		t.writeToTerminals(b)
	}
}

func exitMonitor(args []string, t *Tunnel, m *CLI) {
	logger.Infof("exitMonitor function executed.")
	output := fmt.Sprintf("\r\nDisconnected!\r\n")
	for _, b := range []byte(output) {
		t.writeToTerminals(b)

	}
	m.Used = false
}
