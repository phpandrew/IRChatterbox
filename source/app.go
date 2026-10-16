package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// appVersion is overridden at build time: -ldflags "-X main.appVersion=1.2.3"
var appVersion = "0.1.1"

type App struct {
	ctx     context.Context
	store   *Store
	plugins *Plugins

	mu           sync.Mutex
	conns        map[string]*Conn
	queries      map[string]map[string]bool // server -> lower nick -> open
	focused      bool
	activeServer string
	activeBuffer string
	pendingFocus map[string]bool
	notifyOK     bool
	autoDone     bool
	testHook     func(Event) // used by tests only

	logMu sync.Mutex
}

func NewApp() *App {
	a := &App{
		store:        NewStore(),
		conns:        map[string]*Conn{},
		queries:      map[string]map[string]bool{},
		pendingFocus: map[string]bool{},
		focused:      true,
	}
	a.plugins = NewPlugins(a)
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := wruntime.InitializeNotifications(ctx); err == nil {
		a.notifyOK = true
		wruntime.OnNotificationResponse(ctx, func(r wruntime.NotificationResult) {
			if r.Error != nil {
				return
			}
			info := r.Response.UserInfo
			wruntime.WindowUnminimise(ctx)
			wruntime.WindowShow(ctx)
			wruntime.EventsEmit(ctx, "focusBuffer", map[string]any{"server": info["server"], "buffer": info["buffer"]})
		})
	}
	a.plugins.LoadScripts()
}

func (a *App) domReady(ctx context.Context) {}

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

func (a *App) shutdown(ctx context.Context) {
	msg := a.store.Get().Settings.QuitMessage
	a.mu.Lock()
	conns := make([]*Conn, 0, len(a.conns))
	for _, c := range a.conns {
		conns = append(conns, c)
	}
	a.mu.Unlock()
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *Conn) { defer wg.Done(); c.Disconnect(msg) }(c)
	}
	wg.Wait()
}

func (a *App) secondInstance() {
	if a.ctx != nil {
		wruntime.WindowUnminimise(a.ctx)
		wruntime.WindowShow(a.ctx)
	}
}

// ---------- event plumbing ----------

func (a *App) emit(c *Conn, ev Event) {
	if ev.Extra != nil && ev.Extra["query"] == true {
		a.addQuery(c.cfg.ID, ev.Buffer)
	}
	if a.testHook != nil {
		a.testHook(ev)
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "irc", ev)
	}
	if (ev.Type == "msg" || ev.Type == "event") && ev.Buffer != "" && ev.Buffer != "*active*" {
		a.logEvent(c.cfg.Name, ev)
	}
}

func (a *App) addQuery(server, nick string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.queries[server] == nil {
		a.queries[server] = map[string]bool{}
	}
	a.queries[server][lower(nick)] = true
}

func (a *App) hasQuery(server, nick string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.queries[server][lower(nick)]
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
		if ok, _ := path.Match(mask, lower(prefix)); ok {
			return true
		}
	}
	return false
}

var fmtRe = regexp.MustCompile("\x03(\\d{1,2}(,\\d{1,2})?)?|[\x02\x0F\x16\x1D\x1F\x1E\x11]")

func stripFormatting(s string) string { return fmtRe.ReplaceAllString(s, "") }

func (a *App) notify(c *Conn, buffer, nick, text string, isPM, highlight bool) {
	set := a.store.Get().Settings
	if !set.Notifications || (isPM && !highlight && !set.NotifyPMs) {
		return
	}
	a.mu.Lock()
	visible := a.focused && a.activeServer == c.cfg.ID && strings.EqualFold(a.activeBuffer, buffer)
	a.mu.Unlock()
	if visible || !a.notifyOK {
		return
	}
	title := nick + " in " + buffer
	if isPM {
		title = "Message from " + nick
	}
	body := stripFormatting(text)
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	wruntime.SendNotification(a.ctx, wruntime.NotificationOptions{
		ID:    newID(),
		Title: title,
		Body:  body,
		Data:  map[string]any{"server": c.cfg.ID, "buffer": buffer},
	})
}

// ---------- logging ----------

func (a *App) logPath(server, buffer string) string {
	return filepath.Join(a.store.dir, "logs", safeName(server), safeName(lower(buffer))+".jsonl")
}

func (a *App) logEvent(server string, ev Event) {
	if !a.store.Get().Settings.Logging {
		return
	}
	if ev.Type == "event" && ev.Kind != "topic" && ev.Kind != "kick" && ev.Kind != "nick" {
		return // don't log join/part/quit/mode noise
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	p := a.logPath(server, ev.Buffer)
	os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(ev)
	f.Write(append(b, '\n'))
}

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
	a.logMu.Lock()
	defer a.logMu.Unlock()
	f, err := os.Open(a.logPath(sv.Name, buffer))
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

func (a *App) GetConfig() Config { return a.store.Get() }

func (a *App) GetDataDir() string { return a.store.dir }

func (a *App) OpenDataFolder() {
	if runtime.GOOS == "windows" {
		exec.Command("explorer", a.store.dir).Start()
	}
}

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

func (a *App) SaveSettings(s Settings) {
	a.store.Update(func(c *Config) { c.Settings = s })
}

func (a *App) SavePlugins(p PluginSettings) {
	a.store.Update(func(c *Config) { c.Plugins = p })
}

func (a *App) SetNickColor(nick, color string) {
	a.store.Update(func(c *Config) {
		if color == "" {
			delete(c.NickColors, lower(nick))
		} else {
			c.NickColors[lower(nick)] = color
		}
	})
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "config", a.store.Get())
	}
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
}

func (a *App) SetActive(server, buffer string) {
	a.mu.Lock()
	a.activeServer, a.activeBuffer = server, buffer
	a.mu.Unlock()
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

func (a *App) GetThemes() []Theme       { return a.store.Themes() }
func (a *App) SaveTheme(t Theme) error  { return a.store.SaveTheme(t) }
func (a *App) DeleteTheme(n string) error { return a.store.DeleteTheme(n) }

func (a *App) OpenURL(u string) {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		wruntime.BrowserOpenURL(a.ctx, u)
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

func (a *App) Notify(title, body string) {
	if a.notifyOK {
		wruntime.SendNotification(a.ctx, wruntime.NotificationOptions{ID: newID(), Title: title, Body: body})
	}
}

var _ = fmt.Sprintf
var _ = time.Now
