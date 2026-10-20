package core

// Invite-only channels: a "Perform on connect" line like "/msg InviteBot !invite me" gets you
// invited, and when the invite arrives for a channel in this server's auto-join list, it's
// joined straight away. "/wait 5" in Perform pauses before the next line.

import (
	"strconv"
	"strings"
	"time"
)

const performWaitMax = 60 * time.Second

// performWait reads a "/wait N" (or /delay, /sleep) Perform line: N seconds, decimals allowed.
func performWait(line string) (time.Duration, bool) {
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) == 0 {
		return 0, false
	}
	switch strings.ToLower(f[0]) {
	case "/wait", "/delay", "/sleep":
	default:
		return 0, false
	}
	secs := 1.0
	if len(f) > 1 {
		v, err := strconv.ParseFloat(strings.TrimSuffix(strings.ToLower(f[1]), "s"), 64)
		if err != nil || v < 0 {
			return 0, false
		}
		secs = v
	}
	d := time.Duration(secs * float64(time.Second))
	if d > performWaitMax {
		d = performWaitMax
	}
	return d, true
}

// joinInvited joins channel when we're invited to it and it's one of this server's auto-join
// channels (so an invite from a stranger never drags you anywhere).
func (c *Conn) joinInvited(who, channel string) {
	if !strings.EqualFold(who, c.Nick()) || !c.isChannel(channel) {
		return
	}
	for _, ch := range c.Channels() {
		if strings.EqualFold(ch, channel) {
			return // already in it
		}
	}
	sv, ok := c.app.store.Server(c.cfg.ID)
	if !ok {
		return
	}
	for _, a := range sv.AutoJoin {
		f := strings.Fields(a)
		if len(f) == 0 || !strings.EqualFold(f[0], channel) {
			continue
		}
		line := "JOIN " + channel
		if len(f) > 1 {
			line += " " + f[1]
		}
		c.SendRaw(line)
		return
	}
}
