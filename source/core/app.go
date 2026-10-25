package core

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type App struct {
	ctx     context.Context
	store   *Store
	plugins *Plugins

	syncMu      sync.Mutex // one sync at a time (sync.go)
	syncTimerMu sync.Mutex
	syncTimer   *time.Timer
	syncPolled  time.Time         // last focus-triggered check (SyncPoll)
	syncPending syncPendingSignIn // between "email me a code" and entering it

	mu             sync.Mutex
	conns          map[string]*Conn
	queries        map[string]map[string]string // server -> lower nick -> nick, for open private chats (saved in queries.json)
	focused        bool
	activeServer   string
	activeBuffer   string
	pendingFocus   map[string]bool
	notifyOK       bool
	autoDone       bool
	mentions       int
	autoAwayOn     bool
	lastUpdate     UpdateInfo
	trayStarted    bool
	startMinimized bool
	windowShown    bool
	allowQuit      bool
	closeAsked     time.Time
	parked         map[string]bool // Android: servers disconnected while in the background
	testHook       func(Event)     // used by tests only

	logMu     sync.Mutex
	bncMu     sync.Mutex
	bncOnline map[string]int64 // bouncer server id -> last online (ms), see bouncer.go
	logLines  map[string]int   // path -> line count, for trimming
	logOpened map[string]bool
	lastCrash string // crash report from the previous run (crashlog.go)
}

func NewApp() *App {
	a := &App{
		store:        NewStore(),
		conns:        map[string]*Conn{},
		queries:      map[string]map[string]string{},
		pendingFocus: map[string]bool{},
		focused:      true,
	}
	a.plugins = NewPlugins(a)
	a.loadQueries()
	a.pruneNotifyRules()
	return a
}

// SetAutoJoin makes a channel join automatically whenever the server connects.
func (a *App) SetAutoJoin(server, channel string, on bool) {
	a.store.setAutoJoin(server, channel, on)
}

// SetStartView sets (or clears, with empty values) the channel shown at launch.
func (a *App) SetStartView(server, buffer string) {
	a.store.Update(func(c *Config) { c.StartView = StartBuffer{Server: server, Buffer: buffer} })
}

// AutoConnect connects all servers marked auto-connect. Called by the UI once its
// event listeners are registered, so no early messages are lost. Safe to call repeatedly.
func (a *App) AutoConnect() {
	a.mu.Lock()
	if a.autoDone {
		a.mu.Unlock()
		return
	}
	a.autoDone = true
	a.mu.Unlock()
	for _, s := range a.store.Get().Servers {
		if s.AutoConnect {
			a.Connect(s.ID)
		}
	}
}

// CancelClose is called when the user decides not to quit.
func (a *App) CancelClose() {
	a.mu.Lock()
	a.closeAsked = time.Time{}
	a.mu.Unlock()
}

// ---------- event plumbing ----------

func (a *App) emit(c *Conn, ev Event) {
	if ev.Extra != nil && ev.Extra["query"] == true {
		a.addQuery(c.cfg.ID, ev.Buffer)
	}
	if a.testHook != nil {
		a.testHook(ev)
	}
	if ev.Type == "msg" && !ev.Self && ev.Text != "" {
		ui := ev // the word filter applies to what's shown; logs keep the original
		ui.Text = a.maskFiltered(ev.Text)
		a.emitUI("irc", ui)
	} else {
		a.emitUI("irc", ev)
	}
	if (ev.Type == "msg" || ev.Type == "event" || ev.Type == "redact" || ev.Type == "react") && ev.Buffer != "" && ev.Buffer != "*active*" {
		a.logEvent(c.cfg.Name, ev)
	}
}

func (a *App) addQuery(server, nick string) {
	a.mu.Lock()
	if a.queries[server] == nil {
		a.queries[server] = map[string]string{}
	}
	_, known := a.queries[server][lower(nick)]
	a.queries[server][lower(nick)] = nick
	a.mu.Unlock()
	if !known {
		a.saveQueries()
	}
}

func (a *App) hasQuery(server, nick string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.queries[server][lower(nick)]
	return ok
}

func (a *App) consumePendingFocus(server, ch string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	k := server + "\x00" + lower(ch)
	if a.pendingFocus[k] {
		delete(a.pendingFocus, k)
		return true
	}
	return false
}

func (a *App) markPendingFocus(server, ch string) {
	a.mu.Lock()
	a.pendingFocus[server+"\x00"+lower(ch)] = true
	a.mu.Unlock()
}

// ---------- highlight / ignore / notify ----------

func isWordChar(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func containsWord(text, word string) bool {
	if word == "" {
		return false
	}
	lt, lw := strings.ToLower(text), strings.ToLower(word)
	for i := 0; ; {
		j := strings.Index(lt[i:], lw)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(lw)
		before, after := ' ', ' '
		if start > 0 {
			before = []rune(lt[:start])[len([]rune(lt[:start]))-1]
		}
		if end < len(lt) {
			after = []rune(lt[end:])[0]
		}
		if !isWordChar(before) && !isWordChar(after) {
			return true
		}
		i = start + 1
		if i >= len(lt) {
			return false
		}
	}
}

func (a *App) isHighlight(myNick, text string) bool {
	text = stripFormatting(text)
	if containsWord(text, myNick) {
		return true
	}
	for _, w := range a.store.Get().Settings.HighlightWords {
		if containsWord(text, w) {
			return true
		}
	}
	return false
}

func (a *App) isIgnored(nick, prefix string) bool {
	for _, mask := range a.store.Get().Settings.Ignore {
		mask = lower(mask)
		if mask == lower(nick) {
			return true
		}
		if maskMatch(mask, lower(prefix)) {
			return true
		}
	}
	return false
}

// maskMatch matches an IRC mask (nick!user@host with * and ?) against a prefix. Unlike
// path.Match, * crosses "/", which cloaked hosts like user/alice contain.
func maskMatch(pattern, s string) bool {
	px, sx, star, mark := 0, 0, -1, 0
	for sx < len(s) {
		switch {
		case px < len(pattern) && (pattern[px] == '?' || pattern[px] == s[sx]):
			px++
			sx++
		case px < len(pattern) && pattern[px] == '*':
			star, mark = px, sx
			px++
		case star >= 0:
			px = star + 1
			mark++
			sx = mark
		default:
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}

var fmtRe = regexp.MustCompile("\x03(\\d{1,2}(,\\d{1,2})?)?|[\x02\x0F\x16\x1D\x1F\x1E\x11]")

func stripFormatting(s string) string { return fmtRe.ReplaceAllString(s, "") }

// notify handles a highlight or private message the user isn't looking at:
// plays the chosen sound, flashes the tray icon + taskbar, and shows a Windows toast.
func (a *App) notify(c *Conn, buffer, nick, text string, isPM, highlight bool) {
	set := a.store.Get().Settings
	a.mu.Lock()
	focused := a.focused
	visible := focused && a.activeServer == c.cfg.ID && strings.EqualFold(a.activeBuffer, buffer)
	a.mu.Unlock()
	if visible {
		return
	}
	pmOnly := isPM && !highlight

	sound := set.SoundHighlight
	if pmOnly {
		sound = set.SoundPM
	}
	if sound != "" {
		a.emitUI("playSound", map[string]any{"sound": sound, "volume": set.SoundVolume})
	}
	if set.FlashOnPing && !focused {
		a.raiseAttention()
	}
	if !set.Notifications || (pmOnly && !set.NotifyPMs) || !a.notifyOK {
		return
	}
	title := nick + " in " + buffer
	if isPM {
		title = "Message from " + nick
	}
	body := a.maskFiltered(stripFormatting(text))
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	// silence Windows' own toast sound when we're playing ours
	a.pushToast(title, body, map[string]any{"server": c.cfg.ID, "buffer": buffer}, sound != "")
}

// notifyMissed is the one notification shown after a bouncer replays history containing
// mentions or private messages, instead of a toast per line.
// notifyPresence: a friend on the MONITOR list came online.
func (a *App) notifyPresence(c *Conn, text string) {
	set := a.store.Get().Settings
	if !set.Notifications || !a.notifyOK {
		return
	}
	a.pushToast("Friends", text+" on "+c.cfg.Name, map[string]any{"server": c.cfg.ID, "buffer": ""}, false)
}

func (a *App) notifyMissed(c *Conn, buffer string, n int) {
	set := a.store.Get().Settings
	a.mu.Lock()
	focused := a.focused
	a.mu.Unlock()
	if set.SoundHighlight != "" {
		a.emitUI("playSound", map[string]any{"sound": set.SoundHighlight, "volume": set.SoundVolume})
	}
	if set.FlashOnPing && !focused {
		a.raiseAttention()
	}
	if !set.Notifications || !a.notifyOK {
		return
	}
	what := "1 mention or message"
	if n > 1 {
		what = fmt.Sprintf("%d mentions and messages", n)
	}
	a.pushToast("While you were away", fmt.Sprintf("%s on %s", what, c.cfg.Name), map[string]any{"server": c.cfg.ID, "buffer": buffer}, set.SoundHighlight != "")
}

// raiseAttention flashes the tray icon and taskbar button until the window is focused.
func (a *App) raiseAttention() {
	a.mu.Lock()
	a.mentions++
	n := a.mentions
	a.mu.Unlock()
	tip := fmt.Sprintf("IRChatterbox — %d new mention", n)
	if n != 1 {
		tip += "s"
	}
	setTrayAttention(true, tip)
	if n == 1 {
		flashTaskbar(true)
	}
}

func (a *App) clearAttention() {
	a.mu.Lock()
	had := a.mentions > 0
	a.mentions = 0
	a.mu.Unlock()
	if had {
		setTrayAttention(false, "IRChatterbox")
		flashTaskbar(false)
	}
}

// autoAwayLoop marks you away on every connected network after the configured idle time
// (no keyboard/mouse input anywhere in Windows), and back as soon as you return.
func (a *App) autoAwayLoop() {
	for range time.Tick(15 * time.Second) {
		set := a.store.Get().Settings
		idle := systemIdle()
		a.mu.Lock()
		on := a.autoAwayOn
		conns := make([]*Conn, 0, len(a.conns))
		for _, c := range a.conns {
			conns = append(conns, c)
		}
		a.mu.Unlock()
		limit := time.Duration(set.AutoAwayMinutes) * time.Minute
		switch {
		case !on && set.AutoAway && limit > 0 && idle >= limit:
			msg := set.AutoAwayMessage
			if msg == "" {
				msg = "Away from keyboard"
			}
			for _, c := range conns {
				c.mu.Lock()
				ok := c.status == "connected" && !c.away
				if ok {
					c.autoAway = true
				}
				c.mu.Unlock()
				if ok {
					c.SendRaw("AWAY :" + msg)
					c.info("", "info", fmt.Sprintf("Auto-away: no activity for %d minutes", set.AutoAwayMinutes))
				}
			}
			a.mu.Lock()
			a.autoAwayOn = true
			a.mu.Unlock()
		case on && (idle < 30*time.Second || !set.AutoAway):
			for _, c := range conns {
				c.mu.Lock()
				was := c.autoAway
				c.autoAway = false
				c.mu.Unlock()
				if was && c.Status() == "connected" {
					c.SendRaw("AWAY")
					c.info("", "info", "Welcome back — no longer away")
				}
			}
			a.mu.Lock()
			a.autoAwayOn = false
			a.mu.Unlock()
		}
	}
}

// ---------- logging ----------

// GetHistory returns the last N logged lines for a buffer.
func (a *App) GetHistory(serverID, buffer string) []Event {
	sv, ok := a.store.Server(serverID)
	if !ok {
		return nil
	}
	n := a.store.Get().Settings.HistoryLines
	if n <= 0 {
		return []Event{}
	}
	lines := a.historyTail(sv.Name, buffer, n)
	for i := range lines {
		if lines[i].Type == "msg" && !lines[i].Self {
			lines[i].Text = a.maskFiltered(lines[i].Text)
		}
	}
	return lines
}

// historyTail returns up to n of the newest logged lines for a buffer.
func (a *App) historyTail(serverName, buffer string, n int) []Event {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	f, err := os.Open(a.logPath(serverName, buffer))
	if err != nil {
		return []Event{}
	}
	defer f.Close()
	st, _ := f.Stat()
	const window = 512 * 1024
	if st.Size() > window {
		f.Seek(st.Size()-window, io.SeekStart)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var lines []Event
	first := st.Size() > window
	for sc.Scan() {
		if first {
			first = false
			continue // partial line
		}
		var ev Event
		if json.Unmarshal(sc.Bytes(), &ev) == nil {
			ev.Highlight = false
			lines = append(lines, ev)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// ---------- bound methods (called from the UI) ----------

func (a *App) GetVersion() string { return appVersion }

// GetPlatform tells the UI which host it runs in ("windows", "android", …) so it can hide
// options that don't apply.
func (a *App) GetPlatform() string { return runtime.GOOS }

func (a *App) GetConfig() Config { return a.store.Get() }

// GetDefaultNick suggests a nickname from the Windows account name.
func (a *App) GetDefaultNick() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	name := u.Username
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-[]{}\\`^|", r)) {
			b.WriteRune(r)
		}
	}
	n := b.String()
	if len(n) > 16 {
		n = n[:16]
	}
	if n != "" && unicode.IsDigit(rune(n[0])) {
		n = "_" + n
	}
	return n
}

func (a *App) GetDataDir() string { return a.store.dir }

func (a *App) SaveServer(s ServerConfig) ServerConfig {
	if s.ID == "" {
		s.ID = newID()
	}
	if s.Port == 0 {
		s.Port = 6697
	}
	if s.AutoJoin == nil {
		s.AutoJoin = []string{}
	}
	if s.Perform == nil {
		s.Perform = []string{}
	}
	s.IconText = cleanIconText(s.IconText)
	if s.JoinsParts != "show" && s.JoinsParts != "hide" {
		s.JoinsParts = ""
	}
	a.store.Update(func(c *Config) {
		for i := range c.Servers {
			if c.Servers[i].ID == s.ID {
				c.Servers[i] = s
				return
			}
		}
		c.Servers = append(c.Servers, s)
	})
	a.mu.Lock()
	if c, ok := a.conns[s.ID]; ok {
		c.cfg = s
	}
	a.mu.Unlock()
	return s
}

func (a *App) DeleteServer(id string) {
	a.Disconnect(id)
	a.store.Update(func(c *Config) {
		favs := c.Favourites[:0]
		for _, f := range c.Favourites {
			if f.Server != id {
				favs = append(favs, f)
			}
		}
		c.Favourites = favs
		for k := range c.ChannelJoins {
			if strings.HasPrefix(k, id+"/") {
				delete(c.ChannelJoins, k)
			}
		}
		for i := range c.Servers {
			if c.Servers[i].ID == id {
				c.Servers = append(c.Servers[:i], c.Servers[i+1:]...)
				return
			}
		}
	})
	a.mu.Lock()
	delete(a.conns, id)
	a.mu.Unlock()
}

func (a *App) ReorderServers(ids []string) { a.store.reorderServers(ids) }

func (a *App) SetChannelOrder(server string, order []string) { a.store.setChannelOrder(server, order) }

func (a *App) MoveServer(id string, delta int) {
	a.store.Update(func(c *Config) {
		for i := range c.Servers {
			if c.Servers[i].ID == id {
				j := i + delta
				if j >= 0 && j < len(c.Servers) {
					c.Servers[i], c.Servers[j] = c.Servers[j], c.Servers[i]
				}
				return
			}
		}
	})
}

func (a *App) conn(id string) *Conn {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conns[id]
}

func (a *App) Connect(id string) {
	sv, ok := a.store.Server(id)
	if !ok {
		return
	}
	a.mu.Lock()
	c, ok := a.conns[id]
	if !ok {
		c = newConn(a, sv)
		a.conns[id] = c
	} else {
		c.cfg = sv
	}
	a.mu.Unlock()
	c.Connect()
}

func (a *App) Disconnect(id string) {
	if c := a.conn(id); c != nil {
		c.Disconnect(a.store.Get().Settings.QuitMessage)
	}
}

func (a *App) SaveSettings(s Settings) string {
	old := a.store.Get().Settings
	// a custom sound path must have come from the file picker this session
	if s.SoundHighlight != old.SoundHighlight && isPathLike(s.SoundHighlight) && !pathAllowed(strings.TrimPrefix(s.SoundHighlight, "file:")) {
		s.SoundHighlight = old.SoundHighlight
	}
	if s.SoundPM != old.SoundPM && isPathLike(s.SoundPM) && !pathAllowed(strings.TrimPrefix(s.SoundPM, "file:")) {
		s.SoundPM = old.SoundPM
	}
	a.store.Update(func(c *Config) { c.Settings = s })
	a.updateMonitors(old.NotifyList, s.NotifyList)
	if s.LogMaxLines > 0 && (old.LogMaxLines == 0 || s.LogMaxLines < old.LogMaxLines) {
		go a.TrimAllLogs()
	}
	if hasTray && s.TrayIcon && !a.trayStarted {
		a.trayStarted = true
		a.startTray()
	}
	if err := setAutostart(s.StartWithWindows, s.StartMinimized); err != nil {
		return "Couldn't change Windows startup: " + err.Error()
	}
	return ""
}

func (a *App) SavePlugins(p PluginSettings) {
	cur := a.store.Get().Plugins
	// the now-playing file must have come from the file picker this session
	if p.Music.File != cur.Music.File && p.Music.File != "" && !pathAllowed(p.Music.File) {
		p.Music.File = cur.Music.File
	}
	a.store.Update(func(c *Config) { c.Plugins = p })
	if p.ScriptsEnabled != cur.ScriptsEnabled {
		go a.plugins.LoadScripts() // turning scripts off stops them; on loads them
	}
}

// isPathLike: a file path rather than the name of a built-in sound ("chime", "ping").
func isPathLike(s string) bool { return strings.HasPrefix(s, "file:") }

// baseNick is the person behind a nick, ignoring away/alt suffixes
// (Andrew|Slop, Andrew_, Andrew^, Andrew[afk] → andrew). Must match baseNick in format.js.
func baseNick(nick string) string {
	n := lower(nick)
	if i := strings.IndexByte(n, '|'); i >= 0 {
		n = n[:i]
	}
	if strings.HasSuffix(n, "]") {
		if i := strings.LastIndexByte(n, '['); i >= 0 {
			n = n[:i]
		}
	}
	n = strings.TrimRight(n, "_`^-")
	if n == "" {
		return lower(nick)
	}
	return n
}

// SetNickColor sets a colour for a person — it applies to all their nick variants.
func (a *App) SetNickColor(nick, color string) {
	a.store.Update(func(c *Config) {
		// clear any colour stored for this exact variant so the shared one applies
		delete(c.NickColors, lower(nick))
		if color == "" {
			delete(c.NickColors, baseNick(nick))
		} else if validColor(color) {
			c.NickColors[baseNick(nick)] = color
		}
	})
	a.emitUI("config", a.store.Get())
}

type ServerState struct {
	ID       string         `json:"id"`
	Status   string         `json:"status"`
	Nick     string         `json:"nick"`
	Network  string         `json:"network"`
	Channels []ChannelState `json:"channels"`
}

func (a *App) GetState() []ServerState {
	out := []ServerState{}
	for _, s := range a.store.Get().Servers {
		st := ServerState{ID: s.ID, Status: "disconnected", Nick: s.Nick, Channels: []ChannelState{}}
		if c := a.conn(s.ID); c != nil {
			c.mu.Lock()
			st.Status, st.Nick, st.Network = c.status, c.nick, c.network
			c.mu.Unlock()
			st.Channels = c.ChannelStates()
		}
		out = append(out, st)
	}
	return out
}

func (a *App) SetFocus(f bool) {
	a.mu.Lock()
	a.focused = f
	a.mu.Unlock()
	if f {
		a.clearAttention()
	}
}

func (a *App) SetActive(server, buffer string) {
	a.mu.Lock()
	a.activeServer, a.activeBuffer = server, buffer
	a.mu.Unlock()
}

// MarkRead is called by the UI when you've seen a buffer up to ms. Through a bouncer that
// supports read markers, your other devices then show it as read too.
func (a *App) MarkRead(server, buffer string, ms int64) {
	if c := a.conn(server); c != nil { // servers and bouncers with draft/read-marker
		c.markRead(buffer, ms)
	}
}

func (a *App) OpenQuery(server, nick string) {
	a.addQuery(server, nick)
}

func (a *App) CloseBuffer(server, buffer string) {
	c := a.conn(server)
	if c != nil && c.isChannel(buffer) {
		for _, ch := range c.Channels() {
			if strings.EqualFold(ch, buffer) {
				c.SendRaw("PART " + buffer)
			}
		}
		a.store.rememberChannel(server, buffer, false)
		return
	}
	a.mu.Lock()
	delete(a.queries[server], lower(buffer))
	a.mu.Unlock()
	a.saveQueries()
}

func (a *App) SendInput(server, buffer, text string) {
	c := a.conn(server)
	if c == nil {
		a.Connect(server)
		c = a.conn(server)
		if c == nil {
			return
		}
	}
	if lines := pastedLines(text); lines != nil && buffer != "" && !strings.HasPrefix(buffer, "*") {
		for i := range lines {
			lines[i] = a.expandShortcuts(lines[i])
		}
		if c.SayMultiline(buffer, lines) {
			return // went out as one multi-line message
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(line), "/shortcut") {
			line = a.expandShortcuts(line)
		}
		a.runCommand(c, buffer, line)
	}
}

func (a *App) SaveShortcuts(list []Shortcut) {
	clean := []Shortcut{}
	for _, sc := range list {
		sc.Trigger = strings.TrimSpace(sc.Trigger)
		if sc.Trigger != "" && sc.Text != "" {
			clean = append(clean, sc)
		}
	}
	a.store.Update(func(c *Config) { c.Shortcuts = clean })
	a.pushConfig()
}

// expandShortcuts replaces whole-word shortcut triggers ("$shrug") with their text.
// A trigger must start the line or follow whitespace, and must not be followed by a
// letter, digit or underscore — so "$shrug!" expands but "$shrugging" does not.
func (a *App) expandShortcuts(line string) string {
	list := append([]Shortcut(nil), a.store.Get().Shortcuts...)
	if len(list) == 0 {
		return line
	}
	sort.Slice(list, func(i, j int) bool { return len(list[i].Trigger) > len(list[j].Trigger) })
	var out strings.Builder
	low := strings.ToLower(line)
	for i := 0; i < len(line); {
		if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
			matched := false
			for _, sc := range list {
				t := strings.ToLower(sc.Trigger)
				if t == "" || !strings.HasPrefix(low[i:], t) {
					continue
				}
				end := i + len(t)
				if end < len(line) {
					r, _ := utf8.DecodeRuneInString(line[end:])
					if isWordChar(r) {
						continue
					}
				}
				out.WriteString(sc.Text)
				i = end
				matched = true
				break
			}
			if matched {
				continue
			}
		}
		out.WriteByte(line[i])
		i++
	}
	return out.String()
}

func (a *App) GetThemes() []Theme         { return a.store.Themes() }
func (a *App) SaveTheme(t Theme) error    { return a.store.SaveTheme(t) }
func (a *App) DeleteTheme(n string) error { return a.store.DeleteTheme(n) }

func (a *App) OpenURL(u string) {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		a.openBrowser(u)
	}
}

func (a *App) ReloadPlugins() string {
	return a.plugins.LoadScripts()
}

// TestURLCommand fetches a URL command without sending anything, for the settings preview.
func (a *App) TestURLCommand(cmd URLCommand) string {
	out, err := fetchURLCommand(cmd)
	if err != nil {
		return "Error: " + err.Error()
	}
	return strings.Join(out, "\n")
}

// TestMusic reports what each now-playing source returns, for the settings page.
func (a *App) TestMusic(m MusicSettings) []string {
	var out []string
	if m.File != "" && m.File != a.store.Get().Plugins.Music.File && !pathAllowed(m.File) {
		m.File = "" // only a file the user picked
	}
	for _, src := range []struct{ id, name string }{{"smtc", "Windows media controls"}, {"window", "MusicBee window title"}, {"file", "Text file"}} {
		mm := m
		mm.Source = src.id
		if src.id == "file" && m.File == "" {
			out = append(out, src.name+": not set up")
			continue
		}
		np, err := getNowPlaying(mm)
		if err != nil {
			out = append(out, src.name+": ✗ "+err.Error())
		} else {
			out = append(out, src.name+": ✓ "+np.Text()+"  ("+np.Player+")")
		}
	}
	np, err := getNowPlaying(m)
	if err == nil {
		out = append(out, "", "Reply: "+a.store.Get().Plugins.BotPrefix+np.Format(m.Format))
	}
	return out
}

func (a *App) Notify(title, body string) {
	if a.notifyOK {
		a.pushToast(title, body, nil, a.store.Get().Settings.SoundHighlight != "")
	}
	if snd := a.store.Get().Settings.SoundHighlight; snd != "" {
		a.emitUI("playSound", map[string]any{"sound": snd, "volume": a.store.Get().Settings.SoundVolume})
	}
}

// GetSoundData returns a custom sound file as a data: URL so the UI can play it. Only the
// sound the user picked (or saved) is served: this isn't a general file reader.
func (a *App) GetSoundData(path string) string {
	if !a.soundPathOK(path) {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 8<<20))
	if err != nil {
		return ""
	}
	mime := map[string]string{".wav": "audio/wav", ".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".m4a": "audio/mp4", ".flac": "audio/flac"}[strings.ToLower(filepath.Ext(path))]
	if mime == "" {
		mime = "audio/wav"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

var _ = fmt.Sprintf
var _ = time.Now
