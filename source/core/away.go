package core

// Who's away: away-notify tells us when people go away or come back, but not who was
// already away when we joined. A quiet WHO after joining fills that in (WHOX where the
// server has it, as it's lighter). Its replies are never shown.

import "strings"

const autoWhoMaxUsers = 1000

func (c *Conn) whoForAway(channel string) {
	c.mu.Lock()
	ch, ok := c.channels[lower(channel)]
	if !ok || len(ch.Users) > autoWhoMaxUsers || c.autoWho[lower(channel)] {
		c.mu.Unlock()
		return
	}
	c.autoWho[lower(channel)] = true
	whox := c.whox
	c.mu.Unlock()
	if whox {
		c.SendRaw("WHO " + channel + " %tcnf,153")
	} else {
		c.SendRaw("WHO " + channel)
	}
}

// onAutoWho takes a WHO reply meant for whoForAway. It reports whether it was one.
func (c *Conn) onAutoWho(m *Message) bool {
	var channel, nick, flags string
	switch m.Command {
	case "352": // me channel user host server nick flags :hops realname
		channel, nick, flags = m.Param(1), m.Param(5), m.Param(6)
	case "354": // me 153 channel nick flags
		if m.Param(1) != "153" {
			return false
		}
		channel, nick, flags = m.Param(2), m.Param(3), m.Param(4)
	case "315": // me mask :End of WHO
		channel = m.Param(1)
	}
	c.mu.Lock()
	mine := c.autoWho[lower(channel)]
	if !mine {
		c.mu.Unlock()
		return m.Command == "354" // our token, but the channel's gone: still ours
	}
	if m.Command == "315" {
		delete(c.autoWho, lower(channel))
		c.mu.Unlock()
		c.emitUsersSoon(channel)
		return true
	}
	if ch, ok := c.channels[lower(channel)]; ok {
		if u, ok := ch.Users[lower(nick)]; ok {
			u.Away = strings.HasPrefix(flags, "G")
		}
	}
	c.mu.Unlock()
	return true
}
