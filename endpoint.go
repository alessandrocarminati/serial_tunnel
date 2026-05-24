package main

import "fmt"

type EndpointKind int

const (
	EndpointSerial EndpointKind = iota
	EndpointSSH
)

type Endpoint interface {
	EndpointID() int
	EndpointDescription() string
	EndpointChannels() *[5]chan byte
	EndpointKind() EndpointKind
	EndpointConfig() string
}

func initEndpointChannels(e Endpoint) {
	ch := e.EndpointChannels()
	*ch = [5]chan byte{
		make(chan byte),
		make(chan byte),
		make(chan byte),
		make(chan byte),
		make(chan byte),
	}
}

func (s *Serial) EndpointID() int                  { return s.ID }
func (s *Serial) EndpointDescription() string    { return s.Description }
func (s *Serial) EndpointChannels() *[5]chan byte { return &s.Channels }
func (s *Serial) EndpointKind() EndpointKind      { return EndpointSerial }
func (s *Serial) EndpointConfig() string {
	parity := "N"
	if s.DParity == 1  {
		parity = "P"
	}
	stop := "1"
	switch s.DStop {
	case 0: stop = "1"
	case 1: stop = "1.5"
	case 2: stop = "2"
	default: stop = "U"
	}
	return fmt.Sprintf("ID:%d Name:%s dev=%s speed=%d settings=%d%s%s", s.ID, s.Description, s.Dev, s.DSpeed, s.DBits, parity, stop)
}
