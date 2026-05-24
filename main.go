package main

import (
	"encoding/json"
	"errors"
	"fmt"
	serial "go.bug.st/serial"
	"os"
	"io/ioutil"
	"time"
	"github.com/sirupsen/logrus"
)

const (
	NONE = iota
	ODD
	EVEN
)

const (
	CONTROL = iota
	CINPUT
	COUTPUT
	RTERMINATE
	STERMINATE
)

const defaultConfigFile = "tunnel.json"

type Config struct {
	Debug   bool           `json:"debug"`
	Serials []Serial       `json:"serial"`
	SSH     []SSHEndpoint  `json:"ssh,omitempty"`
	Tunnels []*Tunnel      `json:"-"`
}

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

var logger = logrus.New()

func getConfig() (string, error) {
	switch len(os.Args) {
	case 1:
		return defaultConfigFile, nil

	case 2:
		filePath := os.Args[1]

		info, err := os.Stat(filePath)
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s", filePath)
		}
		if err != nil {
			return "", fmt.Errorf("error checking file: %w", err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("provided path is a directory, not a file: %s", filePath)
		}

		return filePath, nil

	default:
		return "", errors.New("too many arguments: please provide only a single configuration file path")
	}
}

func initialize(configFile string) (*Config, error) {
	type TmpTunnel struct {
		ID            int                 `json:"id"`
		Description   string              `json:"description"`
		Remote        []int               `json:"remote"`
		Terminal      int                 `json:"terminal"`
		SSHTerminal   *SSHTerminalConfig  `json:"ssh_terminal,omitempty"`
		EscapeChar1   byte                `json:"EscapeChar1"`
		EscapeChar2   byte                `json:"EscapeChar2"`
	}

	type TmpConfig struct {
		Debug   bool           `json:"debug"`
		Serials []Serial       `json:"serial"`
		SSH     []SSHEndpoint  `json:"ssh,omitempty"`
		Tunnels []TmpTunnel    `json:"tunnel"`
	}

	var tmpConfig TmpConfig
	var tunnels []*Tunnel

	logger.Debugf("Read config file %s", configFile)
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("error reading JSON file: %v", err)
	}

	logger.Debugf("Parse JSON data")
	endpointMap := make(map[int]Endpoint)
	if err := json.Unmarshal(data, &tmpConfig); err != nil {
		logger.Errorf("fatal: %v", err)
		return nil, err
	}
	logger.Debugf("Parse Success. Here's the data parsed: `%v`", tmpConfig)

	for i := range tmpConfig.Serials {
		s := &tmpConfig.Serials[i]
		logger.Tracef("Considering serial id=%d, Description=%s", s.ID, s.Description)
		endpointMap[s.ID] = s
	}
	for i := range tmpConfig.SSH {
		s := &tmpConfig.SSH[i]
		logger.Tracef("Considering ssh id=%d, Description=%s", s.ID, s.Description)
		endpointMap[s.ID] = s
	}
	logger.Debugf("Endpoints hashed: %d entries", len(endpointMap))

	for _, t := range tmpConfig.Tunnels {
		newTerminal, ok := endpointMap[t.Terminal]
		if !ok {
			return nil, fmt.Errorf("cant find Terminal reference %d in tunnel %d", t.Terminal, t.ID)
		}
		tunnels = append(tunnels, &Tunnel{
			ID:          t.ID,
			Description: t.Description,
			Remote:      make([]*Serial, len(t.Remote)),
			Terminal:    newTerminal,
			SSHTerminal: t.SSHTerminal,
			QuitRequest: make(chan byte),
			MonitorChan: make(chan byte),
			EscapeChar1: t.EscapeChar1,
			EscapeChar2: t.EscapeChar2,
			Debug:       tmpConfig.Debug,
		})
	}
	logger.Debugf("Actual con tunnel structs produced: `%v`", tunnels)

	for i, t := range tmpConfig.Tunnels {
		logger.Tracef("Consider tmpConfig.Tunnels[%d] = %v\n", i, t)
		for j, s := range tmpConfig.Tunnels {
			logger.Tracef("Consider tmpConfig.Serials[%d] = %v\n", j, s)
			for k, d := range s.Remote {
				ep, ok := endpointMap[d]
				if !ok {
					return nil, fmt.Errorf("Remote endpoint with ID %d not found", d)
				}
				serialRemote, ok := ep.(*Serial)
				if !ok {
					return nil, fmt.Errorf("tunnel %d Remote %d must be a serial port, not %T", s.ID, d, ep)
				}
				logger.Tracef("tunnel %d Remote[%d] -> serial %d", j, k, d)
				tunnels[j].Remote[k] = serialRemote
			}
		}
	}

	logger.Debugf("--------------------------------------------------------------------------")
	logger.Debugf("%v", tunnels)
	for _, ep := range endpointMap {
		initEndpointChannels(ep)
	}

	return &Config{
		Debug:   tmpConfig.Debug,
		Serials: tmpConfig.Serials,
		SSH:     tmpConfig.SSH,
		Tunnels: tunnels,
	}, nil
}

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
	<-serialPort.Channels[CONTROL]
	logger.Infof("Quit signal receive for serial %d is done.", serialPort.ID)

	serialPort.Channels[RTERMINATE] <- 0
	serialPort.Channels[STERMINATE] <- 0

	time.Sleep(time.Second)
	logger.Infof("SerialManager for serial %d is stopped.", serialPort.ID)
}

func main() {

	configFn, err := getConfig()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	monitorIn := make(chan byte)
	monitorQuit := make(chan byte)
	monitor := NewCLI(monitorIn, nil, monitorQuit)
	monitor.RegisterCommand("test", "A test command", testHandler)
	monitor.RegisterCommand("exit", "terminatemonitor session", exitMonitor)
	monitor.RegisterCommand("show", "shows configuration items", showHandler)
	go monitor.Run()

	logger.SetLevel(logrus.InfoLevel)
	logger.SetReportCaller(true)

	config, err := initialize(configFn)
	if err != nil {
		panic("error in parsing config")
	}
	if config.Debug {
		logger.SetLevel(logrus.DebugLevel)
		logger.Infof("tunnel debug enabled (escape/intercept/channel routing)")
	}

	logger.Infof("Bring up serials.")
	for _, tunnel := range config.Tunnels {
			logger.Infof("Start tunnel %d (%s) ad Remote", tunnel.ID, tunnel.Description)
			for _, serial := range tunnel.Remote {
				logger.Infof("Start %s (%s) ad Remote", serial.Dev, serial.Description)
				go SerialManager(*serial)
			}
			logger.Infof("Start Terminal id=%d (%s)", tunnel.Terminal.EndpointID(), tunnel.Terminal.EndpointDescription())
			startEndpointManager(tunnel.Terminal)
			logger.Infof("Start tunnel %d manager", tunnel.ID)
			go TunnelManager(tunnel, monitor)
			if tunnel.SSHTerminal != nil {
				startSSHTerminalServer(tunnel, *tunnel.SSHTerminal, monitor)
			}
		}


	time.Sleep(36000*time.Second)

}
