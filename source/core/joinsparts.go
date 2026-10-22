package core

// Showing or hiding joins, parts and quits for one channel, overriding the server's and the
// app-wide setting (Settings → General). The chat view decides what to show; this only
// stores the choice.

import "unicode/utf8"

// SetChannelJoins sets a channel's joins/parts/quits: "show", "hide" or "" (back to the
// server's setting).
func (a *App) SetChannelJoins(server, buffer, mode string) {
	if server == "" || buffer == "" {
		return
	}
	switch mode {
	case "", "show", "hide":
	default:
		return
	}
	a.store.Update(func(c *Config) {
		k := notifyKey(server, buffer)
		if mode == "" {
			delete(c.ChannelJoins, k)
			return
		}
		if c.ChannelJoins == nil {
			c.ChannelJoins = map[string]string{}
		}
		c.ChannelJoins[k] = mode
	})
	a.emitUI("config", a.store.Get())
}

// cleanIconText keeps the sidebar badge to at most 2 visible characters, no spaces.
func cleanIconText(s string) string {
	out := make([]rune, 0, 2)
	for _, r := range s {
		if r == ' ' || r < 0x20 || r == 0x7f {
			continue
		}
		if len(out) == 2 {
			break
		}
		out = append(out, r)
	}
	if !utf8.ValidString(string(out)) {
		return ""
	}
	return string(out)
}

// SetServerJoins sets joins/parts/quits for a whole server: "show", "hide" or "" (as in Settings).
func (a *App) SetServerJoins(server, mode string) {
	if mode != "show" && mode != "hide" {
		mode = ""
	}
	var saved *ServerConfig
	a.store.Update(func(c *Config) {
		for i := range c.Servers {
			if c.Servers[i].ID == server {
				c.Servers[i].JoinsParts = mode
				s := c.Servers[i]
				saved = &s
				return
			}
		}
	})
	if saved == nil {
		return
	}
	a.mu.Lock()
	if c, ok := a.conns[server]; ok {
		c.cfg.JoinsParts = mode
	}
	a.mu.Unlock()
	a.emitUI("config", a.store.Get())
}
