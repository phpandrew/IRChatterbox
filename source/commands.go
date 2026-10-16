package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var helpText = []string{
	"Channel:  /join #chan [key] · /part [#chan] [msg] · /cycle · /topic [text] · /names · /invite nick [#chan] · /list",
	"Messages: /msg nick text · /query nick [text] · /me action · /notice target text · /ctcp nick CMD · /ping nick · /say text",
	"Ops:      /op /deop /voice /devoice /halfop /dehalfop nick… · /kick nick [reason] · /ban nick|mask · /unban mask · /kb nick [reason] · /mode [target] modes",
	"User:     /nick name · /whois nick · /whowas nick · /who mask · /away [msg] · /back · /umode modes · /ignore nick|mask · /unignore nick|mask",
	"Server:   /connect · /reconnect · /disconnect [msg] · /quit [msg] · /server name · /raw LINE (alias /quote) · /motd · /lusers · /version",
	"Services: /ns /cs /ms /hs /os text → NickServ, ChanServ, MemoServ, HostServ, OperServ",
	"Client:   /color nick #hex|reset · /highlight add|del|list word · /clear · /close · /theme name · /reload (plugins) · /help",
	"Anything else is sent raw to the server, e.g. /knock #chan or /sajoin nick #chan.",
}

var hexColor = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$|^#?[0-9a-fA-F]{3}$`)

// runCommand executes one line of input typed into a buffer.
func (a *App) runCommand(c *Conn, buffer, line string) {
	if !strings.HasPrefix(line, "/") || strings.HasPrefix(line, "//") {
		if strings.HasPrefix(line, "//") {
			line = line[1:]
		}
		if buffer == "" {
			c.info("", "error", "You can't talk in the server window. Use /join #channel or /msg nick text.")
			return
		}
		c.Say(buffer, line, false)
		return
	}

	cmd, rest, _ := strings.Cut(line[1:], " ")
	cmd = strings.ToLower(cmd)
	rest = strings.TrimSpace(rest)
	args := strings.Fields(rest)
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	// text after the first n words
	after := func(n int) string {
		s := rest
		for i := 0; i < n; i++ {
			s = strings.TrimLeft(s, " ")
			if j := strings.IndexByte(s, ' '); j >= 0 {
				s = s[j:]
			} else {
				return ""
			}
		}
		return strings.TrimSpace(s)
	}
	inChan := c.isChannel(buffer)
	// commands that may take an optional leading channel argument
	chanArg := func() (string, int) {
		if c.isChannel(arg(0)) {
			return arg(0), 1
		}
		return buffer, 0
	}
	needChan := func() (string, int, bool) {
		ch, n := chanArg()
		if !c.isChannel(ch) {
			c.info(buffer, "error", "/"+cmd+": not in a channel")
			return "", 0, false
		}
		return ch, n, true
	}
	connected := c.Status() == "connected"
	requireConn := func() bool {
		if !connected {
			c.info(buffer, "error", "Not connected. Use /connect")
		}
		return connected
	}

	switch cmd {
	case "help", "commands":
		for _, h := range helpText {
			c.info(buffer, "help", h)
		}

	case "connect", "reconnect":
		if cmd == "reconnect" || connected {
			c.Disconnect("Reconnecting")
			go func() {
				for i := 0; i < 50 && c.Status() != "disconnected"; i++ {
					time.Sleep(100 * time.Millisecond)
				}
				a.Connect(c.cfg.ID)
			}()
		} else {
			a.Connect(c.cfg.ID)
		}
	case "disconnect":
		msg := rest
		if msg == "" {
			msg = a.store.Get().Settings.QuitMessage
		}
		c.Disconnect(msg)
	case "quit":
		msg := rest
		if msg == "" {
			msg = a.store.Get().Settings.QuitMessage
		}
		c.Disconnect(msg)
	case "server":
		for _, s := range a.store.Get().Servers {
			if strings.EqualFold(s.Name, rest) || strings.EqualFold(s.Host, rest) {
				a.Connect(s.ID)
				return
			}
		}
		c.info(buffer, "error", "No saved server named "+rest+". Add it with the + Add server button.")

	case "join", "j":
		if !requireConn() || arg(0) == "" {
			return
		}
		chans := strings.Split(arg(0), ",")
		for i, ch := range chans {
			if !c.isChannel(ch) {
				chans[i] = "#" + ch
			}
			a.markPendingFocus(c.cfg.ID, chans[i])
		}
		l := "JOIN " + strings.Join(chans, ",")
		if arg(1) != "" {
			l += " " + arg(1)
		}
		c.SendRaw(l)
	case "part", "leave":
		ch, n := chanArg()
		if !c.isChannel(ch) {
			return
		}
		msg := after(n)
		if msg == "" {
			msg = a.store.Get().Settings.QuitMessage
		}
		c.SendRaw("PART " + ch + " :" + msg)
		a.store.rememberChannel(c.cfg.ID, ch, false)
	case "cycle", "hop", "rejoin":
		if ch, _, ok := needChan(); ok {
			c.SendRaw("PART " + ch)
			c.SendRaw("JOIN " + ch)
		}
	case "close":
		if inChan {
			c.SendRaw("PART " + buffer)
			a.store.rememberChannel(c.cfg.ID, buffer, false)
		}
		a.CloseBuffer(c.cfg.ID, buffer)
		c.emit(Event{Type: "buffer", Buffer: buffer, Extra: map[string]any{"action": "close"}})

	case "msg", "privmsg", "m":
		if !requireConn() || arg(0) == "" || after(1) == "" {
			return
		}
		c.Say(arg(0), after(1), false)
	case "query", "q":
		if arg(0) == "" {
			return
		}
		a.addQuery(c.cfg.ID, arg(0))
		c.emit(Event{Type: "buffer", Buffer: arg(0), Kind: "query", Extra: map[string]any{"action": "open", "focus": true}})
		if after(1) != "" && requireConn() {
			c.Say(arg(0), after(1), false)
		}
	case "me", "action", "describe":
		if !requireConn() {
			return
		}
		target, text := buffer, rest
		if cmd == "describe" {
			target, text = arg(0), after(1)
		}
		if target == "" {
			return
		}
		c.Action(target, text)
	case "say":
		if buffer != "" && requireConn() {
			c.Say(buffer, rest, false)
		}
	case "notice":
		if requireConn() && arg(0) != "" {
			c.Notice(arg(0), after(1))
		}
	case "ctcp":
		if requireConn() && arg(0) != "" && arg(1) != "" {
			body := strings.ToUpper(arg(1))
			if after(2) != "" {
				body += " " + after(2)
			}
			c.SendRaw("PRIVMSG " + arg(0) + " :\x01" + body + "\x01")
			c.info(buffer, "ctcp", "CTCP "+body+" sent to "+arg(0))
		}
	case "ping":
		if requireConn() && arg(0) != "" {
			c.SendRaw(fmt.Sprintf("PRIVMSG %s :\x01PING %d\x01", arg(0), time.Now().UnixMilli()))
		}

	case "nick":
		if arg(0) != "" {
			if connected {
				c.SendRaw("NICK " + arg(0))
			} else {
				cfg := c.cfg
				cfg.Nick = arg(0)
				a.SaveServer(cfg)
				c.info(buffer, "info", "Nick set to "+arg(0)+" for next connect")
			}
		}
	case "topic", "t":
		ch, n := chanArg()
		if !c.isChannel(ch) {
			return
		}
		if after(n) == "" {
			c.SendRaw("TOPIC " + ch)
		} else {
			c.SendRaw("TOPIC " + ch + " :" + after(n))
		}
	case "kick", "k":
		ch, n, ok := needChan()
		if !ok || arg(n) == "" {
			return
		}
		reason := after(n + 1)
		if reason == "" {
			reason = arg(n)
		}
		c.SendRaw("KICK " + ch + " " + arg(n) + " :" + reason)
	case "ban", "b", "unban", "kb", "kickban":
		ch, n, ok := needChan()
		if !ok || arg(n) == "" {
			return
		}
		target := arg(n)
		mask := target
		if !strings.ContainsAny(mask, "!@*") {
			if uh := c.userHost(target); uh != "" {
				if i := strings.IndexByte(uh, '@'); i >= 0 {
					mask = "*!*" + uh[i:]
				}
			} else {
				mask = target + "!*@*"
			}
		}
		if cmd == "unban" {
			c.SendRaw("MODE " + ch + " -b " + mask)
			return
		}
		c.SendRaw("MODE " + ch + " +b " + mask)
		if cmd == "kb" || cmd == "kickban" {
			reason := after(n + 1)
			if reason == "" {
				reason = "Banned"
			}
			c.SendRaw("KICK " + ch + " " + target + " :" + reason)
		}
	case "op", "deop", "voice", "devoice", "halfop", "dehalfop", "v", "dv", "o", "do":
		ch, n, ok := needChan()
		if !ok {
			return
		}
		nicks := args[n:]
		if len(nicks) == 0 {
			nicks = []string{c.Nick()}
		}
		m := map[string]string{"op": "+o", "o": "+o", "deop": "-o", "do": "-o", "voice": "+v", "v": "+v",
			"devoice": "-v", "dv": "-v", "halfop": "+h", "dehalfop": "-h"}[cmd]
		for i := 0; i < len(nicks); i += 4 {
			batch := nicks[i:min(i+4, len(nicks))]
			c.SendRaw("MODE " + ch + " " + string(m[0]) + strings.Repeat(string(m[1]), len(batch)) + " " + strings.Join(batch, " "))
		}
	case "mode":
		if arg(0) == "" {
			if inChan {
				c.SendRaw("MODE " + buffer)
			}
			return
		}
		if strings.HasPrefix(arg(0), "+") || strings.HasPrefix(arg(0), "-") {
			target := buffer
			if !inChan {
				target = c.Nick()
			}
			c.SendRaw("MODE " + target + " " + rest)
		} else {
			c.SendRaw("MODE " + rest)
		}
	case "umode":
		c.SendRaw("MODE " + c.Nick() + " " + rest)
	case "invite":
		ch := arg(1)
		if ch == "" {
			ch = buffer
		}
		if arg(0) != "" && c.isChannel(ch) {
			c.SendRaw("INVITE " + arg(0) + " " + ch)
		}
	case "names":
		ch := arg(0)
		if ch == "" {
			ch = buffer
		}
		c.SendRaw("NAMES " + ch)
	case "whois", "wi":
		target := arg(0)
		if target == "" && !inChan {
			target = buffer
		}
		if target != "" {
			c.SendRaw("WHOIS " + target + " " + target)
		}
	case "away":
		if rest == "" {
			rest = "Away"
		}
		c.SendRaw("AWAY :" + rest)
	case "back":
		c.SendRaw("AWAY")
	case "raw", "quote":
		if rest != "" {
			c.SendRaw(rest)
			c.info("", "raw", "→ "+rest)
		}
	case "ns", "cs", "ms", "hs", "os", "bs":
		svc := map[string]string{"ns": "NickServ", "cs": "ChanServ", "ms": "MemoServ", "hs": "HostServ", "os": "OperServ", "bs": "BotServ"}[cmd]
		c.SendRaw("PRIVMSG " + svc + " :" + rest)
		shown := rest
		if strings.HasPrefix(strings.ToUpper(rest), "IDENTIFY") || strings.HasPrefix(strings.ToUpper(rest), "REGISTER") {
			shown = strings.Fields(rest)[0] + " ********"
		}
		c.info(buffer, "info", "→ "+svc+": "+shown)

	case "ignore":
		if rest == "" {
			c.info(buffer, "info", "Ignoring: "+strings.Join(a.store.Get().Settings.Ignore, ", "))
			return
		}
		a.store.Update(func(cf *Config) { cf.Settings.Ignore = append(cf.Settings.Ignore, arg(0)) })
		c.info(buffer, "info", "Now ignoring "+arg(0))
		a.pushConfig()
	case "unignore":
		a.store.Update(func(cf *Config) {
			out := []string{}
			for _, x := range cf.Settings.Ignore {
				if !strings.EqualFold(x, arg(0)) {
					out = append(out, x)
				}
			}
			cf.Settings.Ignore = out
		})
		c.info(buffer, "info", "No longer ignoring "+arg(0))
		a.pushConfig()

	case "color", "colour":
		if arg(0) == "" {
			c.info(buffer, "help", "Usage: /color nick #ff8800  ·  /color nick reset")
			return
		}
		col := arg(1)
		if col == "reset" || col == "" {
			a.SetNickColor(arg(0), "")
			c.info(buffer, "info", "Colour for "+arg(0)+" reset")
			return
		}
		if !hexColor.MatchString(col) {
			if named, ok := namedColors[strings.ToLower(col)]; ok {
				col = named
			} else {
				c.info(buffer, "error", "Colour must be a hex value like #ff8800 or a name (red, orange, yellow, green, teal, blue, purple, pink)")
				return
			}
		}
		if !strings.HasPrefix(col, "#") {
			col = "#" + col
		}
		a.SetNickColor(arg(0), col)
		c.info(buffer, "info", "Colour for "+arg(0)+" set to "+col)
	case "highlight", "hl":
		sub := strings.ToLower(arg(0))
		word := after(1)
		switch sub {
		case "add":
			if word != "" {
				a.store.Update(func(cf *Config) { cf.Settings.HighlightWords = append(cf.Settings.HighlightWords, word) })
				c.info(buffer, "info", "Added highlight word: "+word)
			}
		case "del", "remove", "rm":
			a.store.Update(func(cf *Config) {
				out := []string{}
				for _, w := range cf.Settings.HighlightWords {
					if !strings.EqualFold(w, word) {
						out = append(out, w)
					}
				}
				cf.Settings.HighlightWords = out
			})
			c.info(buffer, "info", "Removed highlight word: "+word)
		default:
			c.info(buffer, "info", "Highlight words: "+strings.Join(a.store.Get().Settings.HighlightWords, ", ")+"  (use /highlight add|del word)")
		}
		a.pushConfig()
	case "reload":
		c.info(buffer, "info", a.plugins.LoadScripts())

	default:
		// pass through anything else as a raw command: /knock #chan → KNOCK #chan
		if !requireConn() {
			return
		}
		l := strings.ToUpper(cmd)
		if rest != "" {
			l += " " + rest
		}
		c.SendRaw(l)
	}
}

func (a *App) pushConfig() {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "config", a.store.Get())
	}
}

var namedColors = map[string]string{
	"red": "#e5484d", "orange": "#f76b15", "yellow": "#e2b714", "green": "#30a46c", "teal": "#12a594",
	"cyan": "#05a2c2", "blue": "#3e63dd", "purple": "#8e4ec6", "pink": "#d6409f", "brown": "#ad7f58", "gray": "#8b8d98", "grey": "#8b8d98",
}
