package main

import (
	"fmt"
	"strings"
	"sync"
)

// CommandFunction is the type of functions that can be registered as commands.
type CommandFunction func(args []string, dte *Serial, tunnel *Tunnel, m *CLI)

// Command holds information about a registered command.
type Command struct {
	Name        string
	HelpText    string
	Handler     CommandFunction
}

// CLI represents the command-line interface.
type CLI struct {
	commands   map[string]Command
	stdin      chan byte
	stdout     *chan byte
	sconnected *Serial
	tconnected *Tunnel
	quit       chan byte
	Used       bool
	registerMu sync.Mutex
	ConnectedID int
	DTEToMonitor map[int]bool
}

// NewCLI initializes a new CLI instance.
func NewCLI(stdin chan byte, stdout *chan byte, quit chan byte) *CLI {
	return &CLI{
		commands: make(map[string]Command),
		stdin:    stdin,
		stdout:   stdout,
		quit:     quit,
		Used:     false,
	}
}

// RegisterCommand registers a new command.
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

// ListCommands returns a list of registered commands and their help text.
func (c *CLI) ListCommands() string {
	c.registerMu.Lock()
	defer c.registerMu.Unlock()

	logger.Debugf("Help command issued in the CLI")
	var result strings.Builder
	result.WriteString("Available commands:\n")
	for _, cmd := range c.commands {
		result.WriteString(fmt.Sprintf("%-15s %s\n", cmd.Name, cmd.HelpText))
	}
	return result.String()
}

// Run starts the CLI's main loop.
func (c *CLI) Run() {
	logger.Infof("CLI is alive.")
	go c.inputLoop()
//	c.outputLoop()
}

/*// outputLoop writes to stdout.
func (c *CLI) outputLoop() {
	for {
		select {
		case textByte := <-c.stdout:
			fmt.Print(string(textByte))
		case <-c.quit:
			return
		}
	}
}*/


// inputLoop reads from stdin channel and processes commands.
func (c *CLI) inputLoop() {
	buffer := make([]byte, 0)

	for {
		select {
		case b := <-c.stdin:
			if b == 13 {
				// Simulate Enter key press
				input := string(buffer)
				logger.Infof("CLI received a the command \"%s\".", input)
				c.parseInput(input)
				buffer = nil // reset buffer
			} else {
				buffer = append(buffer, b)
				logger.Tracef("CLI is receiving things: last char= %d current buffer\"%s\"", b, string(buffer))
			}
		case <-c.quit:
			return
		}
	}
}

// parseInput processes the input string.
func (c *CLI) parseInput(input string) {
	args := strings.Fields(input)
	if len(args) == 0 {
		return
	}

	command := args[0]
	switch command {
	case "help":
		for _, b := range []byte(c.ListCommands()) {
			c.sconnected.Channels[CINPUT] <- b
		}
	case "quit":
		c.quit <- 1
	default:
		c.registerMu.Lock()
		cmd, found := c.commands[command]
		c.registerMu.Unlock()

		if found {
			cmd.Handler(args[1:], c.sconnected, c.tconnected, c)
		} else {
			for _, b := range []byte(fmt.Sprintf("Unknown command: %s\n", command)) {
				c.sconnected.Channels[CINPUT] <- b
			}
		}
	}
}

// test command handler function.
func testHandler(args []string, dte *Serial, t *Tunnel, m *CLI) {
	logger.Infof("testHandler function executed.")
	output := fmt.Sprintf("Test command executed with args: %v\n", args)
	for _, b := range []byte(output) {
		dte.Channels[CINPUT] <- b
	}
}


// showHandler handles the 'show' command and its subcommands.
func showHandler(args []string, dte *Serial, t *Tunnel, m *CLI) {
	subcommands := []Command{
		{
			Name:     "current_tunnel_id",
			HelpText: "Show current tunnel ID",
			Handler:  showCurrentTunnelID,
		},
		// Add more subcommands as needed
	}

	logger.Infof("showHandler function executed.")
	if len(args) == 0 || args[0] == "?" {
		help := "Available options for 'show':\n"
		for _, cmd := range subcommands {
			help += fmt.Sprintf("%-25s %s\n", cmd.Name, cmd.HelpText)
		}
		for _, b := range []byte(help) {
			dte.Channels[CINPUT] <- b
		}
		return
	}

	subcommandName := args[0]
	for _, cmd := range subcommands {
		if cmd.Name == subcommandName {
			cmd.Handler(args[1:], dte, t, m)
			return
		}
	}

	logger.Debugf("Unknown \"show\" subcommand requested (%s).", args[0])
	for _, b := range []byte("Unknown option. Use 'show ?' for help.\n") {
	dte.Channels[CINPUT] <- b
	}
}

// 
func showCurrentTunnelID(args []string, dte *Serial, t *Tunnel, m *CLI) {
	// Logic to show current tunnel ID
	logger.Infof("showCurrentTunnelID function executed.")
	output := fmt.Sprintf("Test command executed with args: %v\n", args)
	for _, b := range []byte(output) {
		dte.Channels[CINPUT] <- b
	}
}

// 
func exitMonitor(args []string, dte *Serial, t *Tunnel, m *CLI) {
	// Logic to show current tunnel ID
	logger.Infof("exitMonitor function executed.")
	output := fmt.Sprintf("Disconnected!\n")
	for _, b := range []byte(output) {
		dte.Channels[CINPUT] <- b
	}
	m.DTEToMonitor[dte.ID]=false

}

