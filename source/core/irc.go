package core

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type User struct {
	Nick   string `json:"nick"`
	Prefix string `json:"prefix"` // all prefix symbols held, highest first, e.g. "@+"
	Away   bool   `json:"away"`
}

type Channel struct {
	Name    string
	Topic   string
	TopicBy string
	Modes   string
	Users   map[string]*User // keyed by lower nick
}

type Event struct {
	Type      string         `json:"type"`
	Server    string         `json:"server"`
	Buffer    string         `json:"buffer"`
	Kind      string         `json:"kind,omitempty"`
	Nick      string         `json:"nick,omitempty"`
	Prefix    string         `json:"prefix,omitempty"`
	Text      string         `json:"text,omitempty"`
	Time      int64          `json:"time"`
	Self      bool           `json:"self,omitempty"`
	Highlight bool           `json:"highlight,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

type Conn struct {
	app *App
	cfg ServerConfig

	mu            sync.Mutex
	conn          net.Conn
	out           chan string
	nick          string
	status        string // disconnected | connecting | connected
	registered    bool
	network       string
	prefixModes   string // e.g. "qaohv"
	prefixSymbols string // e.g. "~&@%+"
	chanTypes     string
	chanModes     [4]string
	channels      map[string]*Channel
	hosts         map[string]string // lower nick -> user@host
	caps          map[string]bool
	capLS         []string
	nickTries     int
	wantQuit      bool
	reconnectN    int
	away          bool
	autoAway      bool // away was set by auto-away (so we also unset it)
	lag           time.Duration
	pingSent      time.Time

	userDebounce map[string]*time.Timer
	stop         chan struct{}
	running      bool          // a run() loop exists (connected, connecting or waiting to reconnect)
	wake         chan struct{} // cuts a reconnect wait short

	// bouncer / history state (see bouncer.go)
	connectedAt    time.Time
	batches        map[string]string // open BATCH ref -> type
	historyLimit   int               // ISUPPORT CHATHISTORY
	histFetched    map[string]bool   // targets already fetched this connection
	replayBase     map[string]int64  // lower target -> newest message we had when its history was requested
	lastSeen       map[string]int64  // lower target -> newest non-self message time we have (ms)
	recentSelf     map[string][]selfLine
	readMarks      map[string]int64 // lower target -> read marker (ms)
	replayMentions int
	replayFirst    string // buffer of the first missed mention, opened by the summary toast
	summaryTimer   *time.Timer

	// IRCv3 extras (ircv3.go)
	labelSeq      int
	pending       []pendingMsg // sent, waiting for echo-message
	clientTagDeny string       // ISUPPORT CLIENTTAGDENY
	monitorMax    int          // ISUPPORT MONITOR: -1 = not supported, 0 = no limit
	monInitial    bool         // first MONITOR replies after connecting (no toasts)
	prevOnline    int64        // when this bouncer connection was last online (ms), loaded at connect
	onlineSince   time.Time    // set once registered
}

func newConn(app *App, cfg ServerConfig) *Conn {
	return &Conn{app: app, cfg: cfg, status: "disconnected", prefixModes: "ov", prefixSymbols: "@+",
		chanTypes: "#&", channels: map[string]*Channel{}, hosts: map[string]string{}, caps: map[string]bool{},
		userDebounce: map[string]*time.Timer{}}
}

func lower(s string) string { return strings.ToLower(s) }

func (c *Conn) isChannel(t string) bool {
	return t != "" && strings.ContainsRune(c.chanTypes, rune(t[0]))
}

func (c *Conn) emit(ev Event) {
	ev.Server = c.cfg.ID
	if ev.Time == 0 {
		ev.Time = time.Now().UnixMilli()
	}
	c.app.emit(c, ev)
}

func (c *Conn) info(buffer, kind, text string) {
	c.emit(Event{Type: "info", Buffer: buffer, Kind: kind, Text: text})
}

func (c *Conn) setStatus(s string) {
	c.mu.Lock()
	c.status = s
	nick := c.nick
	net := c.network
	c.mu.Unlock()
	c.emit(Event{Type: "status", Extra: map[string]any{"status": s, "nick": nick, "network": net}})
}

func (c *Conn) Nick() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nick
}

func (c *Conn) Status() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// ---------- connection lifecycle ----------

func (c *Conn) Connect() {
	c.mu.Lock()
	if c.running {
		// Already connected, or waiting to reconnect: never start a second loop (two loops
		// would share one connection's state). If it's waiting, retry now instead.
		if !c.wantQuit {
			select {
			case c.wake <- struct{}{}:
			default:
			}
		}
		c.mu.Unlock()
		if c.wantQuitNow() {
			// the old loop is shutting down after a Disconnect: start fresh once it's gone
			go func() {
				for i := 0; i < 50 && c.isRunning(); i++ {
					time.Sleep(100 * time.Millisecond)
				}
				c.Connect()
			}()
		}
		return
	}
	c.running = true
	c.wantQuit = false
	c.status = "connecting"
	c.stop = make(chan struct{})
	c.wake = make(chan struct{}, 1)
	c.mu.Unlock()
	go c.run()
}

func (c *Conn) isRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

func (c *Conn) wantQuitNow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wantQuit
}

func (c *Conn) Disconnect(msg string) {
	c.mu.Lock()
	c.wantQuit = true
	st := c.status
	stop := c.stop
	c.mu.Unlock()
	if stop != nil {
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
	if st == "connected" {
		c.SendRaw("QUIT :" + msg)
		time.Sleep(300 * time.Millisecond)
	}
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()
}

func (c *Conn) run() {
	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
	}()
	for {
		c.setStatus("connecting")
		addr := net.JoinHostPort(c.cfg.Host, strconv.Itoa(c.cfg.Port))
		c.info("", "info", fmt.Sprintf("Connecting to %s%s…", addr, map[bool]string{true: " (TLS)", false: ""}[c.cfg.TLS]))
		err := c.session(addr)
		c.mu.Lock()
		wasOnline := !c.onlineSince.IsZero()
		c.onlineSince = time.Time{}
		c.mu.Unlock()
		if wasOnline && c.cfg.Bouncer != "" {
			c.app.setLastOnline(c.cfg.ID, time.Now().UnixMilli())
		}
		c.mu.Lock()
		quit := c.wantQuit
		c.registered = false
		c.channels = map[string]*Channel{}
		c.status = "disconnected"
		c.mu.Unlock()
		if err != nil {
			c.info("", "error", "Disconnected: "+err.Error())
			log.Printf("%s: disconnected: %v", c.cfg.Name, err)
		} else {
			c.info("", "info", "Disconnected")
		}
		c.setStatus("disconnected")
		c.emit(Event{Type: "reset"})
		c.app.plugins.Emit("disconnect", c, "", map[string]any{"reason": fmt.Sprint(err)})
		if quit {
			return
		}
		c.reconnectN++
		wait := time.Duration(min(5*c.reconnectN, 60)) * time.Second
		c.info("", "info", fmt.Sprintf("Reconnecting in %ds…", int(wait.Seconds())))
		select {
		case <-time.After(wait):
		case <-c.wake: // Connect pressed (e.g. after fixing the settings): retry now
		case <-c.stop:
			return
		}
		c.mu.Lock()
		if c.wantQuit {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
	}
}

func (c *Conn) session(addr string) error {
	d := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 60 * time.Second}
	var nc net.Conn
	var err error
	if c.cfg.TLS {
		nc, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: c.cfg.Host, InsecureSkipVerify: c.cfg.TLSSkipVerify})
	} else {
		nc, err = d.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	out := make(chan string, 512)
	c.mu.Lock()
	c.conn = nc
	c.out = out
	c.nick = c.cfg.Nick
	if c.nick == "" {
		c.nick = "chatterbox" + newID()[:4]
	}
	c.nickTries = 0
	c.caps = map[string]bool{}
	c.capLS = nil
	c.connectedAt = time.Now()
	c.batches = map[string]string{}
	c.histFetched = map[string]bool{}
	c.replayBase = map[string]int64{}
	c.pending = nil
	c.clientTagDeny = ""
	c.monitorMax = -1
	c.prevOnline = c.app.lastOnline(c.cfg.ID)
	c.historyLimit = 0
	if c.lastSeen == nil {
		c.lastSeen = map[string]int64{}
		c.recentSelf = map[string][]selfLine{}
		c.readMarks = map[string]int64{}
	}
	c.mu.Unlock()

	done := make(chan struct{})
	go c.writer(nc, out, done)
	defer close(done)

	switch {
	case c.cfg.Bouncer == "znc" && c.cfg.BouncerPass != "":
		c.SendRaw("PASS " + c.cfg.bouncerLogin() + ":" + c.cfg.BouncerPass)
	case c.cfg.Bouncer == "" && c.cfg.Password != "":
		c.SendRaw("PASS " + c.cfg.Password)
	}
	c.SendRaw("CAP LS 302")
	c.SendRaw("NICK " + c.nick)
	user := c.cfg.Username
	if c.cfg.Bouncer != "" {
		user = c.cfg.bouncerLogin()
	}
	if user == "" {
		user = c.nick
	}
	real := c.cfg.Realname
	if real == "" {
		real = "IRChatterbox user"
	}
	c.SendRaw(fmt.Sprintf("USER %s 0 * :%s", user, real))

	// keepalive / lag
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				c.mu.Lock()
				c.pingSent = time.Now()
				c.mu.Unlock()
				c.SendRaw("PING :lag" + strconv.FormatInt(time.Now().UnixMilli(), 10))
				if c.cfg.Bouncer != "" && c.Status() == "connected" {
					c.app.setLastOnline(c.cfg.ID, time.Now().UnixMilli()) // survives a crash or power cut
				}
			}
		}
	}()

	r := bufio.NewReaderSize(nc, 64*1024)
	for {
		nc.SetReadDeadline(time.Now().Add(240 * time.Second))
		line, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		line = c.decode(line)
		if debugRaw != nil {
			debugRaw("<< " + strings.TrimRight(line, "\r\n"))
		}
		c.handle(ParseMessage(line))
	}
}

// writer sends lines with simple flood protection (burst of 5, then 1 every 500ms).
func (c *Conn) writer(nc net.Conn, out chan string, done chan struct{}) {
	tokens := 5.0
	last := time.Now()
	for {
		select {
		case <-done:
			return
		case line := <-out:
			if c.cfg.Bouncer != "" {
				// the bouncer paces what it sends upstream itself (soju.im/SAFERATE), so don't
				// hold back history requests behind our own flood protection
				nc.SetWriteDeadline(time.Now().Add(30 * time.Second))
				if _, err := nc.Write([]byte(line + "\r\n")); err != nil {
					nc.Close()
					return
				}
				continue
			}
			now := time.Now()
			tokens = mathMin(5, tokens+now.Sub(last).Seconds()*2)
			last = now
			if tokens < 1 {
				time.Sleep(time.Duration((1 - tokens) / 2 * float64(time.Second)))
				tokens = 1
				last = time.Now()
			}
			tokens--
			nc.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := nc.Write([]byte(line + "\r\n")); err != nil {
				nc.Close()
				return
			}
		}
	}
}

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// debugRaw, when set (tests), sees every line sent and received.
var debugRaw func(string)

func (c *Conn) SendRaw(line string) {
	if debugRaw != nil {
		debugRaw(">> " + line)
	}
	line = c.encode(strings.NewReplacer("\r", "", "\n", " ").Replace(line))
	c.mu.Lock()
	out := c.out
	c.mu.Unlock()
	if out == nil {
		return
	}
	select {
	case out <- line:
	default:
	}
}

// maxPayload returns the maximum safe text length for PRIVMSG/NOTICE to target.
func (c *Conn) maxPayload(cmd, target string) int {
	overhead := len(":") + len(c.Nick()) + 1 + 75 + 1 + len(cmd) + 1 + len(target) + 2 + 2
	n := 510 - overhead
	if n < 100 {
		n = 100
	}
	return n
}

// Say sends a PRIVMSG (splitting long text) and echoes it locally.
func (c *Conn) Say(target, text string, viaPlugin bool) {
	if strings.HasPrefix(target, "=") { // a DCC chat window
		c.app.dccChatSay(c, target, "privmsg", text)
		return
	}
	if c.Status() != "connected" {
		c.info(target, "error", "Not connected")
		return
	}
	for _, chunk := range splitText(text, c.maxPayload("PRIVMSG", target)) {
		// with echo-message the line is shown (and bots see it) when the server echoes it
		if !c.sendMsg("PRIVMSG", target, chunk, viaPlugin) {
			c.emitOwn(target, "privmsg", chunk)
			if !viaPlugin {
				c.app.plugins.OnMessage(c, target, c.Nick(), chunk, true)
			}
		}
	}
}

func (c *Conn) Action(target, text string) {
	if strings.HasPrefix(target, "=") {
		c.app.dccChatSay(c, target, "action", text)
		return
	}
	if !c.sendMsg("PRIVMSG", target, "\x01ACTION "+text+"\x01", false) {
		c.emitOwn(target, "action", text)
	}
}

func (c *Conn) Notice(target, text string) {
	for _, chunk := range splitText(text, c.maxPayload("NOTICE", target)) {
		if !c.sendMsg("NOTICE", target, chunk, false) {
			c.emitOwn(target, "notice", chunk)
		}
	}
}

func (c *Conn) emitOwn(target, kind, text string) {
	buf := target
	if !c.isChannel(target) && !c.hasQuery(target) && kind == "notice" {
		buf = "*active*"
	}
	c.emitOwnTagged(buf, target, kind, text, nil)
}

func (c *Conn) emitOwnTagged(buf, target, kind, text string, extra map[string]any) {
	c.emit(Event{Type: "msg", Buffer: buf, Kind: kind, Nick: c.Nick(), Prefix: c.prefixOf(target, c.Nick()), Text: text, Self: true, Extra: extra})
	c.noteSeen(buf, time.Now().UnixMilli(), true, text)
}

func (c *Conn) hasQuery(nick string) bool {
	return c.app.hasQuery(c.cfg.ID, nick)
}

func (c *Conn) prefixOf(channel, nick string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ch, ok := c.channels[lower(channel)]; ok {
		if u, ok := ch.Users[lower(nick)]; ok && u.Prefix != "" {
			return u.Prefix[:1]
		}
	}
	return ""
}

// ---------- user list ----------

func (c *Conn) emitUsersSoon(channel string) {
	key := lower(channel)
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.userDebounce[key]; ok {
		t.Stop()
	}
	c.userDebounce[key] = time.AfterFunc(120*time.Millisecond, func() { c.emitUsers(channel) })
}

func (c *Conn) emitUsers(channel string) {
	c.mu.Lock()
	ch, ok := c.channels[lower(channel)]
	if !ok {
		c.mu.Unlock()
		return
	}
	users := make([]User, 0, len(ch.Users))
	for _, u := range ch.Users {
		users = append(users, *u)
	}
	name := ch.Name
	c.mu.Unlock()
	sort.Slice(users, func(i, j int) bool { return lower(users[i].Nick) < lower(users[j].Nick) })
	c.emit(Event{Type: "users", Buffer: name, Extra: map[string]any{"users": users}})
}

func (c *Conn) Users(channel string) []User {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.channels[lower(channel)]
	if !ok {
		return nil
	}
	out := []User{}
	for _, u := range ch.Users {
		out = append(out, *u)
	}
	return out
}

// sortPrefix orders prefix symbols by rank.
func (c *Conn) sortPrefix(p string) string {
	syms := []rune(p)
	sort.Slice(syms, func(i, j int) bool {
		return strings.IndexRune(c.prefixSymbols, syms[i]) < strings.IndexRune(c.prefixSymbols, syms[j])
	})
	return string(syms)
}

// stripPrefix splits "@+nick" into ("@+", "nick").
func (c *Conn) stripPrefix(n string) (string, string) {
	i := 0
	for i < len(n) && strings.ContainsRune(c.prefixSymbols, rune(n[i])) {
		i++
	}
	return n[:i], n[i:]
}

// channelsWith returns channels where nick is present.
func (c *Conn) channelsWith(nick string) []string {
	var out []string
	for _, ch := range c.channels {
		if _, ok := ch.Users[lower(nick)]; ok {
			out = append(out, ch.Name)
		}
	}
	return out
}

func (c *Conn) userHost(nick string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hosts[lower(nick)]
}

func (c *Conn) Channels() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []string{}
	for _, ch := range c.channels {
		out = append(out, ch.Name)
	}
	return out
}

type ChannelState struct {
	Name  string `json:"name"`
	Topic string `json:"topic"`
	Modes string `json:"modes"`
	Users []User `json:"users"`
}

func (c *Conn) ChannelStates() []ChannelState {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []ChannelState{}
	for _, ch := range c.channels {
		cs := ChannelState{Name: ch.Name, Topic: ch.Topic, Modes: ch.Modes}
		for _, u := range ch.Users {
			cs.Users = append(cs.Users, *u)
		}
		out = append(out, cs)
	}
	return out
}

// ---------- auth & registration ----------

func (c *Conn) wantedCaps() []string {
	want := []string{"multi-prefix", "away-notify", "account-notify", "server-time", "extended-join", "chghost", "userhost-in-names", "message-tags", "invite-notify", "cap-notify",
		"batch", "draft/chathistory", "draft/read-marker",
		"echo-message", "labeled-response", "setname", "account-tag", "extended-monitor", "draft/message-redaction", "standard-replies",
		encTag} // encTag: our soju build sends encrypted history even on networks without message-tags
	switch {
	case c.cfg.Bouncer == "soju" && c.cfg.BouncerPass != "":
		want = append(want, "sasl")
	case c.cfg.Bouncer == "" && c.cfg.AuthMethod == "sasl" && c.cfg.AuthPass != "":
		want = append(want, "sasl")
	}
	return want
}

func (c *Conn) onRegistered() {
	c.mu.Lock()
	c.registered = true
	c.reconnectN = 0
	c.onlineSince = time.Now()
	c.mu.Unlock()
	c.setStatus("connected")
	c.app.plugins.Emit("connect", c, "", map[string]any{"nick": c.Nick()})
	c.app.restoreQueries(c)
	if c.cfg.Bouncer != "" {
		c.fetchMissedQueries()
	}
	go func() {
		time.Sleep(1500 * time.Millisecond) // after ISUPPORT (005) has said whether MONITOR exists
		c.startMonitor()
	}()
	if c.cfg.Bouncer == "" && c.cfg.AuthMethod == "nickserv" && c.cfg.AuthPass != "" {
		user := c.cfg.AuthUser
		if user != "" {
			c.SendRaw("PRIVMSG NickServ :IDENTIFY " + user + " " + c.cfg.AuthPass)
		} else {
			c.SendRaw("PRIVMSG NickServ :IDENTIFY " + c.cfg.AuthPass)
		}
	}
	go func() {
		if c.cfg.Bouncer == "" && c.cfg.AuthMethod == "nickserv" {
			time.Sleep(2 * time.Second) // give services time to apply cloak before joining
		}
		cfg, _ := c.app.store.Server(c.cfg.ID)
		for _, p := range cfg.Perform {
			p = strings.ReplaceAll(p, "$me", c.Nick())
			c.app.runCommand(c, "", p)
		}
		var chans, keys []string
		for _, a := range cfg.AutoJoin {
			f := strings.Fields(a)
			if len(f) == 0 {
				continue
			}
			chans = append(chans, f[0])
			if len(f) > 1 {
				keys = append(keys, f[1])
			} else {
				keys = append(keys, "x")
			}
		}
		// join channels in batches so the line stays short
		for i := 0; i < len(chans); i += 10 {
			j := min(i+10, len(chans))
			hasKey := false
			for _, k := range keys[i:j] {
				if k != "x" {
					hasKey = true
				}
			}
			line := "JOIN " + strings.Join(chans[i:j], ",")
			if hasKey {
				line += " " + strings.Join(keys[i:j], ",")
			}
			c.SendRaw(line)
		}
	}()
}

func (c *Conn) saslPlain() {
	user, pass := c.cfg.AuthUser, c.cfg.AuthPass
	if user == "" {
		user = c.cfg.Nick
	}
	if c.cfg.Bouncer != "" {
		user, pass = c.cfg.bouncerLogin(), c.cfg.BouncerPass
	}
	payload := base64.StdEncoding.EncodeToString([]byte(user + "\x00" + user + "\x00" + pass))
	for len(payload) >= 400 {
		c.SendRaw("AUTHENTICATE " + payload[:400])
		payload = payload[400:]
	}
	if payload == "" {
		payload = "+"
	}
	c.SendRaw("AUTHENTICATE " + payload)
}

// ---------- incoming ----------

func (c *Conn) handle(m *Message) {
	c.decryptStored(m)
	nick := m.Nick()
	ts := m.Time().UnixMilli()
	if uh := m.UserHost(); uh != "" {
		c.mu.Lock()
		c.hosts[lower(nick)] = uh
		c.mu.Unlock()
	}
	me := c.Nick()
	isMe := strings.EqualFold(nick, me)
	if lbl := m.Tags["label"]; lbl != "" && m.Command != "PRIVMSG" && m.Command != "NOTICE" {
		c.dropLabel(lbl) // an error (e.g. "cannot send to channel") instead of the echo
	}
	replay := c.isReplay(m)
	if replay && m.Command != "PRIVMSG" && m.Command != "NOTICE" {
		return // joins/parts etc. inside a history batch would corrupt the live channel state
	}
	if c.app.plugins.hasHandlers("raw") && !replay {
		c.app.plugins.Emit("raw", c, "", map[string]any{"command": m.Command, "params": m.Params, "nick": nick, "prefix": m.Prefix, "tags": m.Tags})
	}

	switch m.Command {
	case "PING":
		c.SendRaw("PONG :" + m.Trailing())
	case "PONG":
		if strings.HasPrefix(m.Trailing(), "lag") {
			c.mu.Lock()
			c.lag = time.Since(c.pingSent)
			lag := c.lag
			c.mu.Unlock()
			c.emit(Event{Type: "lag", Extra: map[string]any{"ms": lag.Milliseconds()}})
		}
	case "ERROR":
		c.info("", "error", m.Trailing())
	case "TAGMSG":
		c.onTagmsg(m, nick, isMe)
	case "REDACT":
		c.onRedact(m, nick)
	case "SETNAME":
		c.onSetname(m, nick, isMe)
	case "730", "731":
		c.onMonitor(m)
	case "732", "733":
		// MONITOR list replies
	case "734":
		c.info("", "error", "Your friends list is longer than this server allows ("+m.Param(1)+"); the rest aren't watched")
	case "WARN", "NOTE":
		c.info("*active*", "info", strings.Join(m.Params[2:], " "))
	case "ACK":
		// labeled-response: the server got a labeled command that has no other reply
	case "BATCH":
		c.onBatch(m)
	case "MARKREAD":
		c.onMarkRead(m)
	case "CHATHISTORY":
		c.onHistoryTargets(m)
	case "FAIL":
		if m.Param(0) == "CHATHISTORY" || m.Param(0) == "MARKREAD" {
			return // history/read markers unavailable for that target; nothing to show
		}
		c.info("*active*", "error", strings.Join(m.Params[1:], " "))

	case "CAP":
		sub := strings.ToUpper(m.Param(1))
		switch sub {
		case "LS":
			more := m.Param(2) == "*"
			c.capLS = append(c.capLS, strings.Fields(m.Trailing())...)
			if more {
				return
			}
			if c.checkSTS(c.capLS) {
				// plain-text connection to a server that requires TLS: drop it; run() reconnects with TLS
				c.mu.Lock()
				if c.conn != nil {
					c.conn.Close()
				}
				c.mu.Unlock()
				return
			}
			avail := map[string]bool{}
			for _, cp := range c.capLS {
				name, _, _ := strings.Cut(cp, "=")
				avail[name] = true
			}
			var req []string
			for _, w := range c.wantedCaps() {
				if avail[w] {
					req = append(req, w)
				}
			}
			if len(req) == 0 {
				c.SendRaw("CAP END")
			} else {
				c.SendRaw("CAP REQ :" + strings.Join(req, " "))
			}
		case "ACK":
			for _, cp := range strings.Fields(m.Trailing()) {
				c.caps[strings.TrimPrefix(cp, "-")] = !strings.HasPrefix(cp, "-")
			}
			if c.caps["sasl"] && !c.registered {
				c.info("", "info", "Authenticating with SASL…")
				c.SendRaw("AUTHENTICATE PLAIN")
			} else if !c.registered {
				c.SendRaw("CAP END")
			}
		case "NAK":
			if !c.registered {
				c.SendRaw("CAP END")
			}
		}
	case "AUTHENTICATE":
		if m.Param(0) == "+" {
			c.saslPlain()
		}
	case "900":
		c.info("", "info", m.Trailing())
	case "903":
		c.info("", "info", "SASL authentication successful")
		c.SendRaw("CAP END")
	case "902", "904", "905", "906", "907", "908":
		if c.cfg.Bouncer != "" {
			c.info("", "error", "Bouncer login failed: "+m.Trailing()+" (check the bouncer username, password and network)")
		} else {
			c.info("", "error", "SASL: "+m.Trailing())
		}
		c.SendRaw("CAP END")

	case "001":
		c.mu.Lock()
		c.nick = m.Param(0)
		c.mu.Unlock()
		c.info("", "server", m.Trailing())
		c.onRegistered()
	case "005":
		if len(m.Params) < 3 {
			return
		}
		for _, tok := range m.Params[1 : len(m.Params)-1] {
			k, v, _ := strings.Cut(tok, "=")
			switch k {
			case "PREFIX":
				if strings.HasPrefix(v, "(") {
					if i := strings.IndexByte(v, ')'); i > 0 {
						c.mu.Lock()
						c.prefixModes, c.prefixSymbols = v[1:i], v[i+1:]
						c.mu.Unlock()
					}
				}
			case "CHANTYPES":
				c.chanTypes = v
			case "CHANMODES":
				parts := strings.Split(v, ",")
				for i := 0; i < 4 && i < len(parts); i++ {
					c.chanModes[i] = parts[i]
				}
			case "CLIENTTAGDENY":
				c.mu.Lock()
				c.clientTagDeny = v
				c.mu.Unlock()
			case "MONITOR":
				n, _ := strconv.Atoi(v) // no value = no limit
				c.mu.Lock()
				c.monitorMax = n
				c.mu.Unlock()
			case "CHATHISTORY":
				if n, err := strconv.Atoi(v); err == nil {
					c.mu.Lock()
					c.historyLimit = n
					c.mu.Unlock()
				}
			case "NETWORK":
				c.mu.Lock()
				c.network = v
				c.mu.Unlock()
				c.setStatus("connected")
			}
		}
		c.info("", "server", strings.Join(m.Params[1:], " "))
	case "433", "432":
		if !c.registered {
			c.nickTries++
			next := c.cfg.AltNick
			if c.nickTries > 1 || next == "" {
				next = c.cfg.Nick + strings.Repeat("_", c.nickTries)
			}
			c.mu.Lock()
			c.nick = next
			c.mu.Unlock()
			c.info("", "error", fmt.Sprintf("Nick %s unavailable, trying %s", m.Param(1), next))
			c.SendRaw("NICK " + next)
		} else {
			c.info("*active*", "error", m.Param(1)+": "+m.Trailing())
		}

	case "NICK":
		newNick := m.Trailing()
		c.mu.Lock()
		if isMe {
			c.nick = newNick
		}
		var affected []string
		for _, ch := range c.channels {
			if u, ok := ch.Users[lower(nick)]; ok {
				delete(ch.Users, lower(nick))
				u.Nick = newNick
				ch.Users[lower(newNick)] = u
				affected = append(affected, ch.Name)
			}
		}
		c.mu.Unlock()
		for _, ch := range affected {
			c.emit(Event{Type: "event", Kind: "nick", Buffer: ch, Nick: nick, Text: newNick, Time: ts, Self: isMe})
			c.emitUsersSoon(ch)
		}
		c.emit(Event{Type: "rename", Nick: nick, Text: newNick, Self: isMe})
		c.app.plugins.Emit("nick", c, "", map[string]any{"oldNick": nick, "newNick": newNick, "nick": newNick, "self": isMe, "channels": affected})
		if isMe {
			c.setStatus("connected")
			c.info("", "info", "You are now known as "+newNick)
		}

	case "JOIN":
		chName := m.Param(0)
		c.mu.Lock()
		ch, ok := c.channels[lower(chName)]
		if isMe && !ok {
			ch = &Channel{Name: chName, Users: map[string]*User{}}
			c.channels[lower(chName)] = ch
		}
		if ch != nil {
			ch.Users[lower(nick)] = &User{Nick: nick}
		}
		c.mu.Unlock()
		if isMe {
			c.emit(Event{Type: "buffer", Buffer: chName, Kind: "channel", Extra: map[string]any{"action": "open", "focus": c.app.consumePendingFocus(c.cfg.ID, chName)}})
			c.app.store.rememberChannel(c.cfg.ID, chName, true)
			c.SendRaw("MODE " + chName)
			c.fetchHistory(chName)
		}
		// A bouncer re-sends JOIN for every channel it's already in each time you connect; those
		// carry no timestamp (a real join does). Don't show "you joined" for them.
		if !(isMe && c.cfg.Bouncer != "" && m.Tags["time"] == "") {
			c.emit(Event{Type: "event", Kind: "join", Buffer: chName, Nick: nick, Text: m.UserHost(), Time: ts, Self: isMe})
		}
		c.emitUsersSoon(chName)
		c.app.plugins.OnJoin(c, chName, nick, isMe)

	case "PART":
		chName := m.Param(0)
		c.mu.Lock()
		if ch, ok := c.channels[lower(chName)]; ok {
			delete(ch.Users, lower(nick))
			if isMe {
				delete(c.channels, lower(chName))
			}
		}
		c.mu.Unlock()
		reason := ""
		if len(m.Params) > 1 {
			reason = m.Trailing()
		}
		c.emit(Event{Type: "event", Kind: "part", Buffer: chName, Nick: nick, Text: reason, Time: ts, Self: isMe, Extra: map[string]any{"host": m.UserHost()}})
		c.app.plugins.Emit("part", c, chName, map[string]any{"channel": chName, "nick": nick, "reason": reason, "self": isMe, "isChannel": true})
		if isMe {
			c.emit(Event{Type: "buffer", Buffer: chName, Kind: "channel", Extra: map[string]any{"action": "parted"}})
		} else {
			c.emitUsersSoon(chName)
		}

	case "KICK":
		chName, victim := m.Param(0), m.Param(1)
		c.mu.Lock()
		if ch, ok := c.channels[lower(chName)]; ok {
			delete(ch.Users, lower(victim))
			if strings.EqualFold(victim, me) {
				delete(c.channels, lower(chName))
			}
		}
		c.mu.Unlock()
		kickedMe := strings.EqualFold(victim, me)
		c.emit(Event{Type: "event", Kind: "kick", Buffer: chName, Nick: nick, Text: m.Trailing(), Time: ts, Highlight: kickedMe, Extra: map[string]any{"victim": victim}})
		c.app.plugins.Emit("kick", c, chName, map[string]any{"channel": chName, "nick": nick, "victim": victim, "reason": m.Trailing(), "self": kickedMe, "isChannel": true})
		if kickedMe {
			c.emit(Event{Type: "buffer", Buffer: chName, Kind: "channel", Extra: map[string]any{"action": "parted"}})
		} else {
			c.emitUsersSoon(chName)
		}

	case "QUIT":
		c.mu.Lock()
		affected := c.channelsWith(nick)
		for _, chn := range affected {
			delete(c.channels[lower(chn)].Users, lower(nick))
		}
		c.mu.Unlock()
		for _, ch := range affected {
			c.emit(Event{Type: "event", Kind: "quit", Buffer: ch, Nick: nick, Text: m.Trailing(), Time: ts, Extra: map[string]any{"host": m.UserHost()}})
			c.emitUsersSoon(ch)
		}
		if c.hasQuery(nick) {
			c.emit(Event{Type: "event", Kind: "quit", Buffer: nick, Nick: nick, Text: m.Trailing(), Time: ts})
		}
		c.app.plugins.Emit("quit", c, "", map[string]any{"nick": nick, "reason": m.Trailing(), "channels": affected})

	case "AWAY":
		away := len(m.Params) > 0
		c.mu.Lock()
		affected := c.channelsWith(nick)
		for _, chn := range affected {
			c.channels[lower(chn)].Users[lower(nick)].Away = away
		}
		c.mu.Unlock()
		for _, ch := range affected {
			c.emitUsersSoon(ch)
		}

	case "CHGHOST":
		c.mu.Lock()
		c.hosts[lower(nick)] = m.Param(0) + "@" + m.Param(1)
		c.mu.Unlock()

	case "TOPIC":
		chName := m.Param(0)
		c.mu.Lock()
		if ch, ok := c.channels[lower(chName)]; ok {
			ch.Topic = m.Trailing()
			ch.TopicBy = nick
		}
		c.mu.Unlock()
		c.emit(Event{Type: "topic", Buffer: chName, Text: m.Trailing(), Extra: map[string]any{"by": nick}})
		c.emit(Event{Type: "event", Kind: "topic", Buffer: chName, Nick: nick, Text: m.Trailing(), Time: ts, Self: isMe})
		c.app.plugins.Emit("topic", c, chName, map[string]any{"channel": chName, "nick": nick, "topic": m.Trailing(), "self": isMe, "isChannel": true})
	case "332":
		chName := m.Param(1)
		c.mu.Lock()
		if ch, ok := c.channels[lower(chName)]; ok {
			ch.Topic = m.Trailing()
		}
		c.mu.Unlock()
		c.emit(Event{Type: "topic", Buffer: chName, Text: m.Trailing()})
	case "333":
		if c.cfg.Bouncer != "" && c.inBouncerSync() {
			return // the bouncer re-sending channel state on connect, not news
		}
		by := m.Param(2)
		if i := strings.IndexByte(by, '!'); i > 0 {
			by = by[:i]
		}
		when := ""
		if sec, err := strconv.ParseInt(m.Param(3), 10, 64); err == nil {
			when = time.Unix(sec, 0).Format("Jan 2, 2006 15:04")
		}
		c.info(m.Param(1), "info", fmt.Sprintf("Topic set by %s %s", by, when))
	case "331":
		c.emit(Event{Type: "topic", Buffer: m.Param(1), Text: ""})

	case "353":
		chName := m.Param(2)
		c.mu.Lock()
		ch, ok := c.channels[lower(chName)]
		if ok {
			for _, n := range strings.Fields(m.Trailing()) {
				pfx, rest := c.stripPrefix(n)
				name := rest
				if i := strings.IndexByte(rest, '!'); i > 0 {
					name = rest[:i]
					c.hosts[lower(name)] = rest[i+1:]
				}
				ch.Users[lower(name)] = &User{Nick: name, Prefix: c.sortPrefix(pfx)}
			}
		}
		c.mu.Unlock()
		if !ok {
			c.info("*active*", "info", "Names "+chName+": "+m.Trailing())
		}
	case "366":
		c.emitUsersSoon(m.Param(1))

	case "324":
		c.mu.Lock()
		if ch, ok := c.channels[lower(m.Param(1))]; ok {
			ch.Modes = strings.Join(m.Params[2:], " ")
		}
		c.mu.Unlock()
		c.emit(Event{Type: "modes", Buffer: m.Param(1), Text: strings.Join(m.Params[2:], " ")})

	case "MODE":
		target := m.Param(0)
		modeStr := strings.Join(m.Params[1:], " ")
		if c.isChannel(target) {
			c.applyChannelModes(target, m.Params[1:])
			c.emit(Event{Type: "event", Kind: "mode", Buffer: target, Nick: nick, Text: modeStr, Time: ts, Self: isMe})
			c.app.plugins.Emit("mode", c, target, map[string]any{"channel": target, "nick": nick, "modes": modeStr, "self": isMe, "isChannel": true})
			c.SendRaw("MODE " + target)
		} else {
			c.info("", "info", fmt.Sprintf("%s sets mode %s on %s", nick, modeStr, target))
		}

	case "INVITE":
		c.info("*active*", "highlight", fmt.Sprintf("%s invited %s to %s", nick, m.Param(0), m.Param(1)))
		c.app.plugins.Emit("invite", c, "", map[string]any{"channel": m.Param(1), "nick": nick, "invited": m.Param(0)})

	case "PRIVMSG", "NOTICE":
		if replay {
			c.onReplayMessage(m, nick, isMe, ts)
		} else {
			c.onMessage(m, nick, isMe, ts)
		}

	// whois & friends -> active buffer
	case "311":
		c.info("*active*", "whois", fmt.Sprintf("%s is %s@%s (%s)", m.Param(1), m.Param(2), m.Param(3), m.Trailing()))
	case "312":
		c.info("*active*", "whois", fmt.Sprintf("%s is on %s (%s)", m.Param(1), m.Param(2), m.Trailing()))
	case "319":
		c.info("*active*", "whois", fmt.Sprintf("%s is in %s", m.Param(1), m.Trailing()))
	case "317":
		idle, _ := strconv.Atoi(m.Param(2))
		signon := ""
		if s, err := strconv.ParseInt(m.Param(3), 10, 64); err == nil {
			signon = ", signed on " + time.Unix(s, 0).Format("Jan 2 15:04")
		}
		c.info("*active*", "whois", fmt.Sprintf("%s has been idle %s%s", m.Param(1), (time.Duration(idle)*time.Second).String(), signon))
	case "330":
		c.info("*active*", "whois", fmt.Sprintf("%s is logged in as %s", m.Param(1), m.Param(2)))
	case "301":
		c.info("*active*", "whois", fmt.Sprintf("%s is away: %s", m.Param(1), m.Trailing()))
	case "318", "369":
		c.info("*active*", "whois", "— end of WHOIS —")
	case "305", "306":
		c.mu.Lock()
		c.away = m.Command == "306"
		c.mu.Unlock()
		c.info("*active*", "info", m.Trailing())
	case "372", "375", "376", "422":
		c.info("", "motd", m.Trailing())
	case "322":
		c.info("", "list", fmt.Sprintf("%-20s %5s  %s", m.Param(1), m.Param(2), m.Trailing()))
	case "401", "402", "403", "404", "405", "406", "411", "412", "421", "441", "442", "443", "461", "471", "473", "474", "475", "477", "478", "481", "482":
		c.info("*active*", "error", strings.Join(m.Params[1:], " "))
	case "321", "323", "315", "352":
		c.info("", "server", strings.Join(m.Params[1:], " "))
	default:
		if len(m.Command) == 3 && m.Command[0] >= '0' && m.Command[0] <= '9' {
			c.info("", "server", strings.Join(m.Params[1:], " "))
		}
	}
}

func (c *Conn) applyChannelModes(channel string, params []string) {
	if len(params) == 0 {
		return
	}
	modes := params[0]
	args := params[1:]
	ai := 0
	adding := true
	c.mu.Lock()
	ch, ok := c.channels[lower(channel)]
	changed := false
	for _, r := range modes {
		switch {
		case r == '+':
			adding = true
		case r == '-':
			adding = false
		case strings.ContainsRune(c.prefixModes, r):
			if ai >= len(args) {
				continue
			}
			who := args[ai]
			ai++
			if !ok {
				continue
			}
			sym := string(c.prefixSymbols[strings.IndexRune(c.prefixModes, r)])
			if u, ok := ch.Users[lower(who)]; ok {
				if adding && !strings.Contains(u.Prefix, sym) {
					u.Prefix = c.sortPrefix(u.Prefix + sym)
				} else if !adding {
					u.Prefix = strings.ReplaceAll(u.Prefix, sym, "")
				}
				changed = true
			}
		case strings.ContainsRune(c.chanModes[0], r) || strings.ContainsRune(c.chanModes[1], r):
			ai++
		case strings.ContainsRune(c.chanModes[2], r):
			if adding {
				ai++
			}
		}
	}
	c.mu.Unlock()
	if changed {
		c.emitUsersSoon(channel)
	}
}

func (c *Conn) onMessage(m *Message, nick string, isMe bool, ts int64) {
	target := m.Param(0)
	text := m.Trailing()
	kind := strings.ToLower(m.Command)

	// server notices (no nick!user@host prefix)
	if m.Command == "NOTICE" && !strings.Contains(m.Prefix, "!") {
		c.info("", "notice", text)
		return
	}
	if c.app.isIgnored(nick, m.Prefix) {
		return
	}
	// our own message coming back (echo-message), or sent from another of our clients
	skipPlugins := false
	if isMe && c.hasCap("echo-message") {
		if p, found := c.takeEcho(m, target, text); found {
			skipPlugins = p.viaPlugin // a bot's own reply: don't let bots answer it
		}
	}

	// CTCP
	if len(text) > 1 && text[0] == 1 {
		body := strings.Trim(text, "\x01")
		cmd, arg, _ := strings.Cut(body, " ")
		cmd = strings.ToUpper(cmd)
		if cmd == "DCC" {
			if !isMe && m.Command == "PRIVMSG" && !c.isChannel(target) {
				c.onDCC(nick, m.Prefix, arg)
			}
			return
		}
		if cmd == "ACTION" {
			kind = "action"
			text = arg
		} else {
			if isMe {
				return // our own CTCP request echoed back: never answer ourselves
			}
			if m.Command == "PRIVMSG" {
				switch cmd {
				case "VERSION":
					c.SendRaw("NOTICE " + nick + " :\x01VERSION IRChatterbox " + appVersion + "\x01")
				case "PING":
					c.SendRaw("NOTICE " + nick + " :\x01PING " + arg + "\x01")
				case "TIME":
					c.SendRaw("NOTICE " + nick + " :\x01TIME " + time.Now().Format(time.RFC1123) + "\x01")
				case "CLIENTINFO":
					c.SendRaw("NOTICE " + nick + " :\x01CLIENTINFO ACTION CLIENTINFO PING TIME VERSION\x01")
				}
				c.info("*active*", "ctcp", fmt.Sprintf("CTCP %s from %s %s", cmd, nick, arg))
			} else {
				c.info("*active*", "ctcp", fmt.Sprintf("CTCP %s reply from %s: %s", cmd, nick, arg))
			}
			return
		}
	}

	buffer := target
	isPM := !c.isChannel(target)
	if isPM {
		if isMe {
			buffer = target
		} else {
			buffer = nick
		}
		// notices from services etc. go to active buffer instead of opening a query
		if m.Command == "NOTICE" && !c.hasQuery(buffer) {
			buffer = "*active*"
		}
	}

	highlight := false
	if !isMe {
		highlight = c.app.isHighlight(c.Nick(), text)
	}
	ev := Event{Type: "msg", Buffer: buffer, Kind: kind, Nick: nick, Prefix: c.prefixOf(target, nick), Text: text, Time: ts, Self: isMe, Highlight: highlight}
	ev.Extra = msgExtra(m)
	if isPM && buffer != "*active*" {
		ev.Extra["query"] = true
	}
	c.emit(ev)
	c.noteSeen(buffer, ts, isMe, text)
	if skipPlugins {
		return
	}

	if !isMe && c.app.wantsNotify(c, buffer, kind, isPM, highlight) {
		c.app.notify(c, buffer, nick, text, isPM, highlight)
	}
	if m.Command == "PRIVMSG" && kind == "privmsg" {
		replyTo := target
		if isPM {
			replyTo = nick
		}
		c.app.plugins.OnMessage(c, replyTo, nick, text, isMe)
	} else if kind == "action" || kind == "notice" {
		replyTo := target
		if isPM {
			replyTo = nick
		}
		c.app.plugins.Emit(kind, c, replyTo, map[string]any{
			"channel": replyTo, "nick": nick, "text": stripFormatting(text), "raw": text, "self": isMe,
			"isChannel": !isPM, "isPM": isPM,
		})
	}
}

// msgExtra carries per-message IRCv3 tags to the UI: msgid (for deleting) and account.
func msgExtra(m *Message) map[string]any {
	x := map[string]any{}
	if id := m.Tags["msgid"]; id != "" {
		x["msgid"] = id
	}
	if acc := m.Tags["account"]; acc != "" && acc != "*" {
		x["account"] = acc
	}
	if r := replyTag(m); r != "" {
		x["reply"] = r
	}
	return x
}
