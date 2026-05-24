# Serial Tunnel Utility
The Serial Tunnel Utility is a Go application designed to act as a 
proxy/gateway between a number of serial devices. It supports the creation 
of multiple tunnels, where a tunnel consists of connected remotes and one 
terminal. The utility provides a flexible and configurable way to manage 
serial communication.

## Features
Multiple Serial Devices: Configure and manage multiple remotes (network 
appliances/server uarts) and one physical terminal.

SSH terminal: Optional embedded SSH server so a remote operator can use the 
same console hub as the local VT100 (one client at a time per tunnel).

SSH remote: Optional remote serial line reached over SSH instead of a local 
serial port.

Tunnel Configuration: Define tunnels that connect a set of device consoles 
with one local serial terminal, and optionally an SSH terminal.

Escape Sequence: From a terminal, send `EscapeChar1` then a digit `0`–`9` to 
select which device console is active. All terminals (serial and SSH) share 
the same selection. A separate escape from a device console opens the 
configuration monitor.

Text Interface Monitor: Access a text interface monitor that allows 
configuration changes, both local and system-wide. System-wide configuration 
includes defining physical interfaces.

## Getting Started
Prerequisites
* Go installed on your machine.

## Installation
Build the application:
```
go build
```
or
```
GOARCH=arm go build
```
to build a different target.

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
NOTE: IDs are numeric and must be unique across `serial` and `ssh` entries.

### SSH remote

Add an `ssh` array entry and reference its `id` as the tunnel `DCE`:

```
"ssh": [
    {
        "id": 10,
        "description": "Pi console",
        "host": "192.168.1.50",
        "port": 22,
        "user": "pi",
        "keyfile": "~/.ssh/id_rsa",
        "remote_command": "cat /dev/ttyAMA0"
    }
],
"tunnel": [
    {"id": 0, "description": "lab", "DTE": [0, 1], "DCE": 10, "EscapeChar1": 13, "EscapeChar2": 65 }
]
```

| Field | Description |
|-------|-------------|
| `host` | SSH server hostname or IP |
| `port` | SSH port (default 22) |
| `user` | Login user (default `root`) |
| `password` | Password auth (optional if `keyfile` is set) |
| `keyfile` | Path to private key (supports `~`) |
| `remote_command` | Command run on connect (e.g. `cat /dev/ttyUSB0`). If omitted, an interactive shell is started |
| `connect_timeout` | Dial timeout in seconds (default 10) |

DTE endpoints must remain local serial ports.

### SSH terminal

Add `ssh_terminal` to a tunnel. The hub listens for **one** SSH client; a 
second connection is rejected until the first disconnects.

```
"tunnel": [
    {
        "id": 0,
        "description": "lab",
        "DTE": [0, 1],
        "DCE": 2,
        "EscapeChar1": 1,
        "EscapeChar2": 97,
        "ssh_terminal": {
            "listen": "0.0.0.0:2222",
            "host_key_file": "ssh_host_key",
            "password": "tunnel",
            "authorized_keys_file": "authorized_keys"
        }
    }
]
```

| Field | Description |
|-------|-------------|
| `listen` | TCP address to bind (e.g. `0.0.0.0:2222`) |
| `host_key_file` | Server host key PEM path (created if missing) |
| `password` | Password authentication (optional) |
| `authorized_keys_file` | OpenSSH `authorized_keys` file (optional) |

Connect with a normal SSH client (`ssh -p 2222 user@hub`). Use the same 
escape + digit sequence as on the serial terminal to switch consoles.

