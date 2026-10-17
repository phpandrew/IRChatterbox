package core

// IRCv3 extras: echo-message + labeled-response (what you send is shown as the server
// confirmed it), sts (servers that require TLS), setname, account-tag, +typing, MONITOR
// (friends online) and draft/message-redaction (deleting messages).

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// pendingMsg is something we sent and are waiting to see echoed back (echo-message).
type pendingMsg struct {
	label     string
	target    string
	text      string
	viaPlugin bool
	at        time.Time
}

func (c *Conn) hasCap(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caps[name]
}

// sendMsg sends a PRIVMSG or NOTICE. With echo-message the server sends it back and it's
// shown then (with the server's time and msgid); without it, it's shown straight away.
// It reports whether the line will come back as an echo.
func (c *Conn) sendMsg(cmd, target, text string, viaPlugin bool) bool {
	c.mu.Lock()
	echo := c.caps["echo-message"]
	labeled := c.caps["labeled-response"]
	label := ""
	if echo {
		c.labelSeq++
		if labeled {
			label = "icb" + strconv.Itoa(c.labelSeq)
		}
		// drop anything that never came back
		keep := c.pending[:0]
		for _, p := range c.pending {
			if time.Since(p.at) < 30*time.Second {
				keep = append(keep, p)
			}
		}
		c.pending = append(keep, pendingMsg{label: label, target: target, text: text, viaPlugin: viaPlugin, at: time.Now()})
	}
	c.mu.Unlock()
	line := cmd + " " + target + " :" + text
	if label != "" {
		line = "@label=" + label + " " + line
	}
	c.SendRaw(line)
	return echo
}

// takeEcho matches an echoed message of ours to what we sent. found=false means it came
// from another client on the same account (your phone through the bouncer).
func (c *Conn) takeEcho(m *Message, target, text string) (p pendingMsg, found bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	label := m.Tags["label"]
	for i, q := range c.pending {
		if (label != "" && q.label == label) || (label == "" && strings.EqualFold(q.target, target) && q.text == text) {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			return q, true
		}
	}
	return pendingMsg{}, false
}

// dropLabel forgets a pending message whose label got an error reply instead of an echo.
func (c *Conn) dropLabel(label string) {
	if label == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, q := range c.pending {
		if q.label == label {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			return
		}
	}
}

// ---------- STS ----------

// checkSTS looks at the sts capability in CAP LS. On a plain-text connection the server is
// saying "use TLS on this port": the server's settings are switched to TLS for good and we
// reconnect. Returns true when the connection should be dropped for that upgrade.
func (c *Conn) checkSTS(caps []string) bool {
	if c.cfg.TLS {
		return false
	}
	for _, cp := range caps {
		name, val, _ := strings.Cut(cp, "=")
		if name != "sts" {
			continue
		}
		port := 0
		for _, kv := range strings.Split(val, ",") {
			k, v, _ := strings.Cut(kv, "=")
			if k == "port" {
				port, _ = strconv.Atoi(v)
			}
		}
		if port <= 0 || port > 65535 {
			return false
		}
		c.info("", "info", fmt.Sprintf("This server requires a secure connection (STS): switching to TLS on port %d.", port))
		log.Printf("%s: STS upgrade to TLS port %d", c.cfg.Name, port)
		c.cfg.TLS, c.cfg.Port = true, port
		if sv, ok := c.app.store.Server(c.cfg.ID); ok {
			sv.TLS, sv.Port = true, port
			c.app.SaveServer(sv)
			c.app.emitUI("config", c.app.store.Get())
		}
		c.mu.Lock()
		c.reconnectN = -1 // reconnect straight away
		c.mu.Unlock()
		return true
	}
	return false
}

// ---------- typing ----------

// typingAllowed: the server may deny client tags (ISUPPORT CLIENTTAGDENY=*,-typing etc.).
func (c *Conn) typingAllowed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.caps["message-tags"] {
		return false
	}
	deny := c.clientTagDeny
	allowed := true
	for _, t := range strings.Split(deny, ",") {
		switch t {
		case "*":
			allowed = false
		case "typing", "draft/typing":
			allowed = false
		case "-typing", "-draft/typing":
			allowed = true
		}
	}
	return allowed
}

// SendTyping is called by the UI as you type ("active"), pause ("paused") or send/clear ("done").
func (a *App) SendTyping(server, target, state string) {
	c := a.conn(server)
	if c == nil || c.Status() != "connected" || target == "" || !a.store.Get().Settings.SendTyping {
		return
	}
	if state != "active" && state != "paused" && state != "done" {
		return
	}
	if !c.typingAllowed() {
		return
	}
	c.SendRaw("@+typing=" + state + " TAGMSG " + target)
}

func (c *Conn) onTagmsg(m *Message, nick string, isMe bool) {
	state := m.Tags["+typing"]
	if state == "" {
		state = m.Tags["+draft/typing"]
	}
	if state == "" || isMe {
		return
	}
	target := m.Param(0)
	buffer := target
	if !c.isChannel(target) {
		buffer = nick
	}
	c.emit(Event{Type: "typing", Buffer: buffer, Nick: nick, Text: state})
}

// ---------- MONITOR ----------

func (c *Conn) monitorLimit() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.monitorMax
}

// startMonitor watches the friends list after connecting (servers with MONITOR only).
func (c *Conn) startMonitor() {
	if c.monitorLimit() < 0 {
		return
	}
	nicks := cleanNicks(c.app.store.Get().Settings.NotifyList)
	if len(nicks) == 0 {
		return
	}
	c.mu.Lock()
	c.monInitial = true
	c.mu.Unlock()
	c.monitorAdd(nicks)
	go func() {
		time.Sleep(3 * time.Second)
		c.mu.Lock()
		c.monInitial = false
		c.mu.Unlock()
	}()
}

func (c *Conn) monitorAdd(nicks []string) {
	if lim := c.monitorLimit(); lim > 0 && len(nicks) > lim {
		nicks = nicks[:lim]
	}
	for i := 0; i < len(nicks); i += 20 {
		c.SendRaw("MONITOR + " + strings.Join(nicks[i:min(i+20, len(nicks))], ","))
	}
}

func cleanNicks(list []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range list {
		n = strings.TrimSpace(n)
		if n == "" || strings.ContainsAny(n, " ,!@") || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		out = append(out, n)
	}
	return out
}

// updateMonitors applies a changed friends list to every connected server.
func (a *App) updateMonitors(old, now []string) {
	oldSet := map[string]bool{}
	for _, n := range cleanNicks(old) {
		oldSet[strings.ToLower(n)] = true
	}
	var add, del []string
	nowSet := map[string]bool{}
	for _, n := range cleanNicks(now) {
		nowSet[strings.ToLower(n)] = true
		if !oldSet[strings.ToLower(n)] {
			add = append(add, n)
		}
	}
	for _, n := range cleanNicks(old) {
		if !nowSet[strings.ToLower(n)] {
			del = append(del, n)
		}
	}
	if len(add) == 0 && len(del) == 0 {
		return
	}
	a.mu.Lock()
	conns := make([]*Conn, 0, len(a.conns))
	for _, c := range a.conns {
		conns = append(conns, c)
	}
	a.mu.Unlock()
	for _, c := range conns {
		if c.Status() != "connected" || c.monitorLimit() < 0 {
			continue
		}
		if len(del) > 0 {
			c.SendRaw("MONITOR - " + strings.Join(del, ","))
		}
		if len(add) > 0 {
			c.monitorAdd(add)
		}
	}
}

// onMonitor handles 730 (online) and 731 (offline).
func (c *Conn) onMonitor(m *Message) {
	online := m.Command == "730"
	var nicks []string
	for _, t := range strings.Split(m.Trailing(), ",") {
		if n, _, _ := strings.Cut(t, "!"); n != "" {
			nicks = append(nicks, n)
		}
	}
	if len(nicks) == 0 {
		return
	}
	c.mu.Lock()
	initial := c.monInitial
	c.mu.Unlock()
	list := strings.Join(nicks, ", ")
	if initial {
		// the replies to our MONITOR + arrive together; anything after that is real news
		time.AfterFunc(500*time.Millisecond, func() {
			c.mu.Lock()
			c.monInitial = false
			c.mu.Unlock()
		})
		if online {
			c.info("", "info", "Friends online now: "+list)
		}
		return
	}
	verb := "is"
	if len(nicks) > 1 {
		verb = "are"
	}
	if online {
		c.info("", "highlight", fmt.Sprintf("%s %s now online", list, verb))
		c.emit(Event{Type: "presence", Nick: list, Text: "online"})
		if c.app.store.Get().Settings.NotifyFriends {
			c.app.notifyPresence(c, list+" "+verb+" online")
		}
	} else {
		c.info("", "info", fmt.Sprintf("%s %s now offline", list, verb))
		c.emit(Event{Type: "presence", Nick: list, Text: "offline"})
	}
}

// ---------- message redaction ----------

// Redact deletes one of your messages (draft/message-redaction). Returns an error message.
func (a *App) Redact(server, target, msgid string) string {
	c := a.conn(server)
	if c == nil || c.Status() != "connected" {
		return "Not connected"
	}
	if !c.hasCap("draft/message-redaction") {
		return "This server doesn't support deleting messages"
	}
	if msgid == "" || strings.ContainsAny(msgid, " \r\n") {
		return "That message can't be deleted"
	}
	c.SendRaw("REDACT " + target + " " + msgid)
	return ""
}

// onRedact: REDACT <target> <msgid> [:reason]
func (c *Conn) onRedact(m *Message, nick string) {
	target, msgid := m.Param(0), m.Param(1)
	if msgid == "" {
		return
	}
	buffer := target
	if !c.isChannel(target) {
		if strings.EqualFold(nick, c.Nick()) {
			buffer = target
		} else {
			buffer = nick
		}
	}
	reason := ""
	if len(m.Params) > 2 {
		reason = m.Trailing()
	}
	c.emit(Event{Type: "redact", Buffer: buffer, Nick: nick, Text: msgid, Extra: map[string]any{"reason": reason}})
}

// ---------- setname ----------

func (c *Conn) onSetname(m *Message, nick string, isMe bool) {
	if isMe {
		c.info("*active*", "info", "Your real name is now: "+m.Trailing())
	}
}
