# Serial Tunnel Utility
The Serial Tunnel Utility is a Go application designed to act as a 
proxy/gateway between a number of serial devices. It supports the creation 
of multiple tunnels, where a tunnel consists of connected Data Terminal 
Equipment (DTE) and one Data Communication Equipment (DCE). The utility 
provides a flexible and configurable way to manage serial communication.

## Features
Multiple Serial Devices: Configure and manage multiple DTE (Data Terminal 
Equipment) and one DCE (Data Communication Equipment) devices.

Tunnel Configuration: Define tunnels that connect a set of DTEs with a 
single DCE, facilitating communication between them.

Escape Sequence: Utilize a special escape sequence emitted by DTEs to 
redirect the traffic to a monitoring channel. Only the DTE that emits 
the escape sequence is redirected, while other devices in the same tunnel 
continue normal operation.

Text Interface Monitor: Access a text interface monitor that allows 
configuration changes, both local and system-wide. System-wide configuration 
includes defining physical interfaces, roles (DTE or DCE), and specifying 
the number and composition of tunnels.

## Getting Started
Prerequisites
* Go installed on your machine.
## Installation
Clone the repository:
```
git clone https://github.com/alessandrocarminati/serial_tunnel.git
```
Change into the project directory:
```
cd serial_tunnel
```
Build the application:
```
go build
```
Run the application:
```
./serial_tunnel
```

# Configuration
Create a JSON configuration `tunnel.json` to define the serial devices and 
tunnels. 
See the example configuration.

```
{
    "serial": [
        {"id": 0, "description": "Pippo",      "device": "/dev/ttyS0", "speed": 9600, "parity": 0, "stop": 0, "bits": 8 },
        {"id": 1, "description": "Pluto",      "device": "/dev/ttyS1", "speed": 9600, "parity": 0, "stop": 0, "bits": 8 },
        {"id": 2, "description": "Paperino",   "device": "/dev/ttyS2", "speed": 9600, "parity": 0, "stop": 0, "bits": 8 },
        {"id": 3, "description": "Gastone",    "device": "/dev/ttyS3", "speed": 9600, "parity": 0, "stop": 0, "bits": 8 },
        {"id": 4, "description": "Clarabella", "device": "/dev/ttyS4", "speed": 115200, "parity": 0, "stop": 0, "bits": 8 }
    ],
    "tunnel": [
        {"id": 0, "description": "primo", "DTE": [0, 1], "DCE": 2, "EscapeChar1": 13, "EscapeChar2": 65 },
        {"id": 1, "description": "primo", "DTE": [3], "DCE": 4, "EscapeChar1": 13, "EscapeChar2": 65 }
    ]
}
```
NOTE: IDs are numeric and must be progrssive numbers starting from 0
