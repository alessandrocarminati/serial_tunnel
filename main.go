package main

import (
	//	"context"
	"encoding/json"
	"fmt"
	serial "go.bug.st/serial"
	"io/ioutil"
	//	"os"
	//	"sync"
	"time"
	"github.com/sirupsen/logrus"
)

const (
	NONE = iota
	ODD
	EVEN
)

// Constants for channel indices
const (
	CONTROL = iota
	CINPUT
	COUTPUT
	RTERMINATE
	STERMINATE
)

// Config represents the configuration of the application
type Config struct {
	Serials []Serial `json:"serial"`
	Tunnels []Tunnel `json:"tunnel"`
}

// Serial represents a DTE or DCE interface
type Serial struct {
	ID          int          `json:"id"`
	Description string       `json:"description"`
	Channels    [5]chan byte `json:"-"`
	Dev         string       `json:"device"`
	DSpeed      int          `json:"speed"`
	DBits       int          `json:"bits"`
	DParity     int          `json:"parity"`
	DStop       int          `json:"stop"`
}

// Tunnel represents a set of connected DTEs and one DCE
type Tunnel struct {
	ID          int       `json:"id"`
	Description string    `json:"description"`
	DTE         []*Serial `json:"DTE"`
	DCE         *Serial   `json:"DCE"`
	QuitRequest chan byte `json:"-"`
	MonitorChan chan byte `json:"-"`
	EscapeChar1 byte      `json:"EscapeChar1"`
	EscapeChar2 byte      `json:"EscapeChar2"`
}

var logger = logrus.New()

// init initializes the application using the provided JSON configuration file.
func initialize(configFile string) (*Config, error) {
	// Config represents the configuration of the application
	// Tunnel represents a set of connected DTEs and one DCE
	type TmpTunnel struct {
		ID          int       `json:"id"`
		Description string    `json:"description"`
		DTE         []int     `json:"DTE"`
		DCE         int       `json:"DCE"`
		QuitRequest chan byte `json:"-"`
		CLIChan chan byte `json:"-"`
		EscapeChar1 byte      `json:"EscapeChar1"`
		EscapeChar2 byte      `json:"EscapeChar2"`
	}

	type TmpConfig struct {
		Serials []Serial    `json:"serial"`
		Tunnels []TmpTunnel `json:"tunnel"`
	}

	var tmpConfig TmpConfig
	var tunnels []Tunnel

	logger.Debugf("Read config file %s", configFile)
	// Read JSON file
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("error reading JSON file: %v", err)
	}

	logger.Debugf("Parse JSON data")
	var serialMap = make(map[int]*Serial)
	if err := json.Unmarshal(data, &tmpConfig); err != nil {
		logger.Errorf("fatal: %v", err)
		return nil, err
	}
	logger.Debugf("Parse Success. Here's the data parsed: `%v`", tmpConfig)

	for i, s := range tmpConfig.Serials {
		logger.Tracef("Considering serial #%d, id=%d, Description=%s", i, s.ID, s.Description)
		serialMap[s.ID] = &tmpConfig.Serials[i]
	}
	logger.Debugf("Serials successfully hashed. Here's the hash: %v", serialMap)

	//copy tmp tunnel into the intended tunnel structure
	for _, t := range tmpConfig.Tunnels {
		if newDCE, ok := serialMap[t.DCE]; ok {
			tunnels = append(tunnels, Tunnel{
				ID:          t.ID,
				Description: t.Description,
				DTE:         make([]*Serial, len(t.DTE)),
				DCE:         newDCE,
				QuitRequest: make(chan byte),
				MonitorChan: make(chan byte),
				EscapeChar1: t.EscapeChar1,
				EscapeChar2: t.EscapeChar2,
			})
		} else {
			return nil, fmt.Errorf("cant find DCE reference in tunnel %d", t.ID)
		}
	}
	logger.Debugf("Actual con tunnel structs produced: `%v`", tunnels)

	for i, t := range tmpConfig.Tunnels {
		logger.Tracef("Consider tmpConfig.Tunnels[%d] = %v\n", i, t)
		for j, s := range tmpConfig.Tunnels {
			logger.Tracef("Consider tmpConfig.Serials[%d] = %v\n", j, s)
			for k, d := range s.DTE {
				// Check if the sID exists in the serialMap
				if p, ok := serialMap[d]; ok {
					logger.Tracef("look for %d, found %p\n", d, p)
					// Link the t2 to the corresponding t1 instance
					logger.Tracef("data at now: i=%d, j=%d, s.id=%d, p=%p\n", i, j, s.ID, p)
					tunnels[j].DTE[k] = p
				} else {
					return nil, fmt.Errorf("serial instance with ID %d not found", s.ID)
				}
			}
		}
	}

	logger.Debugf("--------------------------------------------------------------------------")
	logger.Debugf("%v", tunnels)
	// Initialize channels for each Serial and Tunnel
	for i := range tmpConfig.Serials {
		tmpConfig.Serials[i].Channels = [5]chan byte{
			make(chan byte),
			make(chan byte),
			make(chan byte),
			make(chan byte),
			make(chan byte),
		}
	}

	return &Config{
		Serials: tmpConfig.Serials,
		Tunnels: tunnels,
	}, nil
}

// SerialManager manages the communication on a serial port.
func SerialManager(serialPort Serial) {

	mode := &serial.Mode{
		BaudRate: serialPort.DSpeed,
		Parity:   serial.Parity(serialPort.DParity),
		DataBits: serialPort.DBits,
		StopBits: serial.StopBits(serialPort.DStop), // warning 0-> 1 bit stop, 1 -> 1.5 bit stop, 2 -> 2 bit sto
	}

	logger.Infof("starting Serial Manager on port %d - dev \"%s\" with description \"%s\", mode %v", serialPort.ID, serialPort.Dev, serialPort.Description, mode)
	port, err := serial.Open(serialPort.Dev, mode)
	if err != nil {
		logger.Errorf("Error opening serial port: %v", err)
		return
	}
	defer port.Close()

	logger.Debugf("starting Read thread for port %d", serialPort.ID)

	// Read from the serial port and route data to the out channel.
	go func() {
		logger.Debugf("receive thread for port %d is alive", serialPort.ID)
		for {
			select {
			case <-serialPort.Channels[RTERMINATE]:
				logger.Infof("Killing Serial %d receive thread\n", serialPort.ID)
				return
			default:
				logger.Tracef("port #%d default read handler", serialPort.ID)
				buf := make([]byte, 1)
				_, err := port.Read(buf)
				logger.Tracef("port #%d is Reading things", serialPort.ID)

				if err != nil {
					logger.Errorf("Error reading from serial port: %v", err)
					return
				}
				logger.Tracef("port #%d Reading send to channel", serialPort.ID)
				serialPort.Channels[COUTPUT] <- buf[0]
			}
		}
	}()

	logger.Debugf("starting send thread for port %d", serialPort.ID)
	// Route data from in channel to serial port.
	go func() {
		logger.Debugf("send thread for port %d is alive", serialPort.ID)
		for {
			select {
			case data := <-serialPort.Channels[CINPUT]:
				_, err := port.Write([]byte{data})
				if err != nil {
					logger.Errorf("Error writing to serial port: %v", err)
					return
				}
			case <-serialPort.Channels[STERMINATE]:
				logger.Infof("Killing Serial %d send thread.\n", serialPort.ID)
				return
			}
		}
	}()

	logger.Debugf("all work for Serial %d is done.", serialPort.ID)
	// Wait for termination signal.
	<-serialPort.Channels[CONTROL]
	logger.Infof("Quit signal receive for serial %d is done.", serialPort.ID)

	// Signal the cancellation of the goroutines.

	// dispatch terminate to the send and receive thread of a given serial
	serialPort.Channels[RTERMINATE] <- 0
	serialPort.Channels[STERMINATE] <- 0

	// Wait for both spawned goroutines to finish before returning.
	// This ensures that both goroutines have completed their work before SerialManager routine exits.
	time.Sleep(time.Second)
	logger.Infof("SerialManager for serial %d is stopped.", serialPort.ID)
}

// TunnelManager manages communication between DTEs and one DCE in a tunnel.
func TunnelManager(tunnel Tunnel, monitor *CLI) {
	var escaped bool = false

	DTEToMonitor := make(map[int]bool)

	// Handle incoming data from DTEs.
	for _, dte := range tunnel.DTE {
		go func(dte Serial) {
			for {
				select {
				case data := <-dte.Channels[COUTPUT]:
					// Check if it's the escape sequence to connect to the monitor.
					if byte(data) == tunnel.EscapeChar1 && !escaped && !DTEToMonitor[dte.ID] {
						escaped = true
						continue
					}
					if escaped {
						if byte(data) == tunnel.EscapeChar2 {
							if !monitor.Used {
								DTEToMonitor[dte.ID] = true // come se esce da sta trappola?
								logger.Infof("DTE %d connected to monitor\n", dte.ID)
								monitor.stdout = &dte.Channels[CINPUT]
								continue
							} else {
								logger.Warnf("DTE %d request monitor but monitor is already used.\n", dte.ID)
								continue
							}
							escaped = false
						} else {
							tunnel.DCE.Channels[COUTPUT] <- tunnel.EscapeChar1
							escaped = false
						}
					}
					// Route data to DCE.
					if !DTEToMonitor[dte.ID] {
						tunnel.DCE.Channels[CINPUT] <- data
					} else {
						monitor.stdin <- data
					}
				case <-dte.Channels[RTERMINATE]:
					logger.Infof("Stopping DTE %d routine.\n", dte.ID)
					return
				}
			}
		}(*dte)
	}

	// Handle incoming data from DCE.
	go func(dce Serial) {
		for {
			select {
			case data := <-dce.Channels[COUTPUT]:
				// Route data to DTEs.
				for _, dte := range tunnel.DTE {
					dte.Channels[CINPUT] <- data
				}
			case <-dce.Channels[CONTROL]:
				logger.Infof("Stopping DCE routine.\n")
				return
			}
		}
	}(*tunnel.DCE)

	// Wait for termination signal.
	<-tunnel.QuitRequest

	// Signal the cancellation of the goroutines.
	for _, dte := range tunnel.DTE {
		dte.Channels[CONTROL] <- 0
	}
	tunnel.DCE.Channels[CONTROL] <- 0

	// Wait for both spawned goroutines to finish before returning.
	// This ensures that both goroutines have completed their work before SerialManager routine exits.
	time.Sleep(time.Second)
	logger.Infof("tunnel routine is stopped.")
}

func main() {

	monitorIn := make(chan byte)
//	monitorOut := make(chan byte)
	monitorQuit := make(chan byte)
	monitor := NewCLI(monitorIn, nil, monitorQuit)
	monitor.RegisterCommand("Test", "A test command", testHandler)
	go monitor.Run()

	logger.SetLevel(logrus.TraceLevel)
	logger.SetReportCaller(true)

	config, err := initialize("tunnel.json")
	if err != nil {
		panic("error in parsing config")
	}
//	fmt.Println(config)

/*
	for _, t := range config.Tunnels {
		fmt.Printf("Tunnel #%d, Description=%s escape1=%d escape2=%d\n", t.ID, t.Description, t.EscapeChar1, t.EscapeChar2)
		for j, s := range t.DTE {
			fmt.Printf("DTE serial #%d, id=%d, Description=%s, Dev=%s, DSpeed=%d, DBits=%d, DParity=%d, DStop=%d\n", j, s.ID, s.Description, s.Dev, s.DSpeed, s.DBits, s.DParity, s.DStop)
		}
		fmt.Printf("DCE serial id=%d, Description=%s, Dev=%s, DSpeed=%d, DBits=%d, DParity=%d, DStop=%d\n", t.DCE.ID, t.DCE.Description, t.DCE.Dev, t.DCE.DSpeed, t.DCE.DBits, t.DCE.DParity, t.DCE.DStop)
	}
*/

//	SerialManager(*config.Tunnels[0].DTE[0])


	logger.Infof("Bring up serials.")
	for _, tunnel := range config.Tunnels {
			logger.Infof("Start tunnel %d (%s) ad DTE", tunnel.ID, tunnel.Description)
			for _, serial := range tunnel.DTE {
				logger.Infof("Start %s (%s) ad DTE", serial.Dev, serial.Description)
				go SerialManager(*serial)
			}
			logger.Infof("Start %s (%s) ad DCE", tunnel.DCE.Dev, tunnel.DCE.Description)
			go SerialManager(*tunnel.DCE)
			logger.Infof("Start tunnel %d monitor", tunnel.ID)
			go TunnelManager(tunnel, monitor)
		}


	// go interactiveMonitor()
	time.Sleep(36000*time.Second)

}
