package main

import "fmt"

func formatByte(b byte) string {
	switch b {
	case '\r':
		return "0x0d CR"
	case '\n':
		return "0x0a LF"
	case '\t':
		return "0x09 TAB"
	default:
		if b >= 32 && b < 127 {
			return fmt.Sprintf("0x%02x '%c'", b, b)
		}
		return fmt.Sprintf("0x%02x", b)
	}
}

func (t *Tunnel) tunnelRouteDebug(channel string, b byte, escaped, intercept bool, action string) {
	if !t.Debug {
		return
	}
	logger.Debugf(
		"tunnel=%d channel=%s byte=%s escape=%v intercept=%v active_dte=%d esc1=%s esc2=%s action=%s",
		t.ID, channel, formatByte(b), escaped, intercept, t.getActiveIndex(),
		formatByte(t.EscapeChar1), formatByte(t.EscapeChar2), action,
	)
}
