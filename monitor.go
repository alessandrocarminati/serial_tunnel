package main

import (
	"fmt"
	"strings"
	"sync"
)

// CommandFunction is the type of functions that can be registered as commands.
type CommandFunction func(args []string, stdout chan<- byte)

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
	quit       chan byte
	Used       bool
	registerMu sync.Mutex
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
			*c.stdout <- b
		}
	case "quit":
		c.quit <- 1
	default:
		c.registerMu.Lock()
		cmd, found := c.commands[command]
		c.registerMu.Unlock()

		if found {
			cmd.Handler(args[1:], *c.stdout)
		} else {
			for _, b := range []byte(fmt.Sprintf("Unknown command: %s\n", command)) {
				*c.stdout <- b
			}
		}
	}
}



// test command handler function.
func testHandler(args []string, stdout chan<- byte) {
	output := fmt.Sprintf("Test command executed with args: %v\n", args)
	for _, b := range []byte(output) {
		stdout <- b
	}
}
