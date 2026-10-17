package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// JavaScript plugins: every *.js file in <data folder>\plugins (except ones starting with "_")
// runs in its own sandboxed JS engine. See docs/PLUGINS.md for the full API.
//
// Each script has a single worker goroutine, so its handlers, commands and timers run one
// at a time and in order — scripts never need locks.

type job struct {
	fn     goja.Callable
	args   []any
	conn   *Conn
	target string
}

type script struct {
	p        *Plugins
	name     string // file name, e.g. karma.js
	vm       *goja.Runtime
	handlers map[string][]goja.Callable
	commands map[string]goja.Callable // lower-case command name → handler
	jobs     chan job
	done     chan struct{}

	// context of the event currently running (used by reply(), me(), …)
	cur    *Conn
	curTgt string

	timerMu   sync.Mutex
	timers    map[int64]*time.Timer
	nextTimer int64

	storePath string
	store     map[string]any

	menus []jsMenu
}

// jsMenu is a right-click menu item added with menu(where, label, fn).
type jsMenu struct {
	kinds []string
	path  []string // "Fun/Slap" → submenu Fun, item Slap
	fn    goja.Callable
}

// pluginEvents is the list of events scripts can listen for (documented in docs/PLUGINS.md).
var pluginEvents = []string{"message", "action", "notice", "join", "part", "quit", "kick", "nick", "topic", "mode", "invite", "connect", "disconnect", "raw", "load"}

func (p *Plugins) pluginDir() string { return filepath.Join(p.app.store.dir, "plugins") }

func (p *Plugins) LoadScripts() string {
	// stop the old generation
	p.mu.Lock()
	old := p.scripts
	p.scripts = nil
	p.mu.Unlock()
	for _, s := range old {
		s.stop()
	}

	files, _ := filepath.Glob(filepath.Join(p.pluginDir(), "*.js"))
	sort.Strings(files)
	var loaded []*script
	var names, errs []string
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasPrefix(base, "_") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		s, err := p.newScript(base, string(src))
		if err != nil {
			errs = append(errs, base+": "+err.Error())
			continue
		}
		loaded = append(loaded, s)
		names = append(names, base)
	}
	p.mu.Lock()
	p.scripts = loaded
	p.mu.Unlock()
	for _, s := range loaded {
		for _, fn := range s.handlers["load"] {
			s.enqueue(job{fn: fn, args: []any{map[string]any{}}})
		}
	}
	mNames, mErrs := p.mirc.load(p.pluginDir())
	names = append(names, mNames...)
	errs = append(errs, mErrs...)
	if len(mNames) > 0 {
		p.mirc.runStart(p.firstConnected())
	}
	msg := fmt.Sprintf("Plugins loaded: %d script(s) %s", len(names), strings.Join(names, ", "))
	if len(errs) > 0 {
		msg += " — errors: " + strings.Join(errs, "; ")
	}
	return msg
}

// Emit sends an event to every script listening for it. target is the channel or query
// the event belongs to (what reply() answers to); data becomes the event object.
func (p *Plugins) Emit(event string, c *Conn, target string, data map[string]any) {
	if !p.app.store.Get().Plugins.ScriptsEnabled {
		return
	}
	p.mu.Lock()
	scripts := append([]*script(nil), p.scripts...)
	p.mu.Unlock()
	if data == nil {
		data = map[string]any{}
	}
	if c != nil {
		data["server"] = c.cfg.Name
		data["serverId"] = c.cfg.ID
		data["me"] = c.Nick()
	}
	if target != "" {
		data["target"] = target
	}
	for _, s := range scripts {
		for _, fn := range s.handlers[event] {
			s.enqueue(job{fn: fn, args: []any{data}, conn: c, target: target})
		}
	}
	if p.mirc.nEvents.Load() > 0 {
		p.mirc.dispatch(event, c, target, data)
	}
}

func (p *Plugins) hasHandlers(event string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.scripts {
		if len(s.handlers[event]) > 0 {
			return true
		}
	}
	return false
}

// RunCommand runs a /command registered by a plugin. Returns false if none matches.
func (p *Plugins) RunCommand(c *Conn, buffer, name, args string) bool {
	if !p.app.store.Get().Plugins.ScriptsEnabled {
		return false
	}
	if p.mirc.runAlias(c, buffer, name, args) {
		return true
	}
	p.mu.Lock()
	scripts := append([]*script(nil), p.scripts...)
	p.mu.Unlock()
	for _, s := range scripts {
		if fn, ok := s.commands[strings.ToLower(name)]; ok {
			e := map[string]any{
				"command": name, "args": args, "argv": strings.Fields(args),
				"channel": buffer, "target": buffer, "server": c.cfg.Name, "serverId": c.cfg.ID, "me": c.Nick(),
				"isChannel": c.isChannel(buffer),
			}
			s.enqueue(job{fn: fn, args: []any{e}, conn: c, target: buffer})
			return true
		}
	}
	return false
}

// PluginCommands lists commands registered by plugins (for /help and completion).
func (a *App) PluginCommands() []string {
	a.plugins.mu.Lock()
	defer a.plugins.mu.Unlock()
	out := []string{}
	for _, s := range a.plugins.scripts {
		for name := range s.commands {
			out = append(out, name)
		}
	}
	out = append(out, a.plugins.mirc.aliasNames()...)
	sort.Strings(out)
	return out
}

func (s *script) enqueue(j job) {
	select {
	case s.jobs <- j:
	case <-s.done:
	default:
		s.log("event queue full — dropping an event (is a handler stuck?)")
	}
}

func (s *script) run() {
	for {
		select {
		case <-s.done:
			return
		case j := <-s.jobs:
			s.exec(j)
		}
	}
}

func (s *script) exec(j job) {
	defer func() {
		if r := recover(); r != nil {
			s.log(fmt.Sprintf("crashed: %v", r))
		}
	}()
	s.cur, s.curTgt = j.conn, j.target
	if s.cur == nil {
		s.cur = s.p.firstConnected()
	}
	t := time.AfterFunc(15*time.Second, func() { s.vm.Interrupt("timed out after 15 seconds") })
	defer t.Stop()
	args := make([]goja.Value, len(j.args))
	for i, a := range j.args {
		args[i] = s.vm.ToValue(a)
	}
	if _, err := j.fn(goja.Undefined(), args...); err != nil {
		s.log(err.Error())
		s.vm.ClearInterrupt()
	}
}

func (s *script) stop() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.timerMu.Lock()
	for _, t := range s.timers {
		t.Stop()
	}
	s.timers = map[int64]*time.Timer{}
	s.timerMu.Unlock()
	s.vm.Interrupt("plugin unloaded")
}

func (s *script) log(text string) {
	c := s.cur
	if c == nil {
		c = s.p.firstConnected()
	}
	if c != nil {
		c.info("", "plugin", "["+s.name+"] "+text)
	}
}

func (p *Plugins) firstConnected() *Conn {
	p.app.mu.Lock()
	defer p.app.mu.Unlock()
	for _, sv := range p.app.store.Get().Servers {
		if c, ok := p.app.conns[sv.ID]; ok && c.Status() == "connected" {
			return c
		}
	}
	return nil
}

func (s *script) saveStore() {
	b, _ := json.MarshalIndent(s.store, "", "  ")
	os.MkdirAll(filepath.Dir(s.storePath), 0o755)
	tmp := s.storePath + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, s.storePath)
	}
}

func (p *Plugins) newScript(name, src string) (*script, error) {
	s := &script{
		p: p, name: name, vm: goja.New(),
		handlers: map[string][]goja.Callable{}, commands: map[string]goja.Callable{},
		jobs: make(chan job, 256), done: make(chan struct{}), timers: map[int64]*time.Timer{},
		storePath: filepath.Join(p.pluginDir(), "data", strings.TrimSuffix(name, ".js")+".json"),
		store:     map[string]any{},
	}
	if b, err := os.ReadFile(s.storePath); err == nil {
		json.Unmarshal(b, &s.store)
	}
	vm := s.vm
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	cfg := p.app.store.Get().Plugins
	vm.Set("prefix", cfg.BotPrefix)

	// --- events & commands ---
	vm.Set("on", func(call goja.FunctionCall) goja.Value {
		ev := call.Argument(0).String()
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.NewTypeError("on(event, function)"))
		}
		known := false
		for _, e := range pluginEvents {
			known = known || e == ev
		}
		if !known {
			panic(vm.NewTypeError("unknown event %q — see docs/PLUGINS.md", ev))
		}
		s.handlers[ev] = append(s.handlers[ev], fn)
		return goja.Undefined()
	})
	vm.Set("command", func(call goja.FunctionCall) goja.Value {
		name := strings.ToLower(strings.TrimPrefix(call.Argument(0).String(), "/"))
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok || name == "" {
			panic(vm.NewTypeError("command(name, function)"))
		}
		s.commands[name] = fn
		return goja.Undefined()
	})

	// menu("nicklist", "Fun/Slap", function (e) { … }): adds a right-click menu item.
	// where: nicklist, channel, query, status, message, or several separated by commas.
	vm.Set("menu", func(call goja.FunctionCall) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(2))
		label := strings.TrimSpace(call.Argument(1).String())
		if !ok || label == "" {
			panic(vm.NewTypeError("menu(where, label, function)"))
		}
		var kinds []string
		for _, k := range strings.Split(call.Argument(0).String(), ",") {
			k = strings.ToLower(strings.TrimSpace(k))
			if !menuKinds[k] {
				panic(vm.NewTypeError("unknown menu %q — use nicklist, channel, query, status or message", k))
			}
			kinds = append(kinds, k)
		}
		var path []string
		for _, p := range strings.Split(label, "/") {
			if p = strings.TrimSpace(p); p != "" {
				path = append(path, p)
			}
		}
		s.menus = append(s.menus, jsMenu{kinds: kinds, path: path, fn: fn})
		return goja.Undefined()
	})

	// --- sending ---
	vm.Set("reply", func(text string) {
		if s.cur != nil && s.curTgt != "" {
			s.cur.Say(s.curTgt, text, true)
		}
	})
	vm.Set("say", func(target, text string) {
		if s.cur != nil {
			s.cur.Say(target, text, true)
		}
	})
	vm.Set("action", func(target, text string) {
		if s.cur != nil {
			s.cur.Action(target, text)
		}
	})
	vm.Set("notice", func(target, text string) {
		if s.cur != nil {
			s.cur.Notice(target, text)
		}
	})
	vm.Set("raw", func(line string) {
		if s.cur != nil {
			s.cur.SendRaw(line)
		}
	})
	vm.Set("run", func(line string) { // run a command as if typed, e.g. run("/join #test")
		if s.cur != nil {
			s.p.app.runCommand(s.cur, s.curTgt, line)
		}
	})

	// --- information ---
	vm.Set("me", func() string {
		if s.cur != nil {
			return s.cur.Nick()
		}
		return ""
	})
	vm.Set("channels", func() []string {
		if s.cur == nil {
			return []string{}
		}
		ch := s.cur.Channels()
		sort.Strings(ch)
		return ch
	})
	vm.Set("users", func(channel string) []User {
		if s.cur == nil {
			return []User{}
		}
		u := s.cur.Users(channel)
		if u == nil {
			return []User{}
		}
		return u
	})
	vm.Set("isOp", func(channel, nick string) bool {
		if s.cur == nil {
			return false
		}
		for _, u := range s.cur.Users(channel) {
			if strings.EqualFold(u.Nick, nick) {
				return strings.ContainsAny(u.Prefix, "~&@")
			}
		}
		return false
	})
	vm.Set("stripFormatting", stripFormatting)

	// --- output to you ---
	vm.Set("log", func(text string) { s.log(text) })
	vm.Set("notify", func(title, body string) { s.p.app.pushToast(title, body, nil, false) })

	// --- web ---
	vm.Set("httpGet", func(u string) string {
		resp, err := httpClient.Get(u)
		if err != nil {
			return ""
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		return string(b)
	})
	vm.Set("httpGetJSON", func(u string) goja.Value {
		resp, err := httpClient.Get(u)
		if err != nil {
			return goja.Null()
		}
		defer resp.Body.Close()
		var v any
		if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&v) != nil {
			return goja.Null()
		}
		return vm.ToValue(v)
	})

	// --- timers (run on the script's worker, in the context they were created in) ---
	addTimer := func(call goja.FunctionCall, repeat bool) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			panic(vm.NewTypeError("setTimeout(function, ms)"))
		}
		ms := call.Argument(1).ToInteger()
		if repeat && ms < 1000 {
			ms = 1000 // intervals: at most once a second
		}
		if ms < 0 {
			ms = 0
		}
		conn, tgt := s.cur, s.curTgt
		s.timerMu.Lock()
		s.nextTimer++
		id := s.nextTimer
		var t *time.Timer
		var fire func()
		fire = func() {
			select {
			case <-s.done:
				return
			default:
			}
			s.enqueue(job{fn: fn, conn: conn, target: tgt})
			s.timerMu.Lock()
			defer s.timerMu.Unlock()
			if _, alive := s.timers[id]; !alive {
				return
			}
			if repeat {
				t.Reset(time.Duration(ms) * time.Millisecond)
			} else {
				delete(s.timers, id)
			}
		}
		t = time.AfterFunc(time.Duration(ms)*time.Millisecond, fire)
		s.timers[id] = t
		s.timerMu.Unlock()
		return vm.ToValue(id)
	}
	clearTimer := func(id int64) {
		s.timerMu.Lock()
		if t, ok := s.timers[id]; ok {
			t.Stop()
			delete(s.timers, id)
		}
		s.timerMu.Unlock()
	}
	vm.Set("setTimeout", func(call goja.FunctionCall) goja.Value { return addTimer(call, false) })
	vm.Set("setInterval", func(call goja.FunctionCall) goja.Value { return addTimer(call, true) })
	vm.Set("clearTimeout", clearTimer)
	vm.Set("clearInterval", clearTimer)

	// --- persistent storage (plugins/data/<name>.json) ---
	store := vm.NewObject()
	store.Set("get", func(call goja.FunctionCall) goja.Value {
		if v, ok := s.store[call.Argument(0).String()]; ok {
			return vm.ToValue(v)
		}
		return call.Argument(1)
	})
	store.Set("set", func(key string, v goja.Value) {
		s.store[key] = v.Export()
		s.saveStore()
	})
	store.Set("delete", func(key string) {
		delete(s.store, key)
		s.saveStore()
	})
	store.Set("keys", func() []string {
		out := []string{}
		for k := range s.store {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	})
	vm.Set("store", store)

	// --- helpers ---
	vm.Set("toF", func(c float64) float64 { return c*9/5 + 32 })
	vm.Set("toC", func(f float64) float64 { return (f - 32) * 5 / 9 })

	t := time.AfterFunc(5*time.Second, func() { vm.Interrupt("took too long to load") })
	_, err := vm.RunString(src)
	t.Stop()
	if err != nil {
		return nil, err
	}
	go s.run()
	return s, nil
}

const examplePlugin = `// IRChatterbox plugin example — see docs/PLUGINS.md on GitHub for the full API.
// Files in this folder ending in .js are loaded at startup (or with /reload).
// Files starting with "_" (like this one) are skipped — copy it to e.g. "dice.js" to enable.

// !roll → "[Bot] Andrew rolled 4"
on("message", function (e) {
  if (e.text === "!roll") {
    reply(prefix + e.nick + " rolled " + (1 + Math.floor(Math.random() * 6)));
  }
});

// a /slap command:  /slap bob
command("slap", function (e) {
  if (e.argv.length) action(e.channel, "slaps " + e.argv[0] + " around a bit with a large trout");
});

// remember how many times each person has said hello (kept between restarts)
on("message", function (e) {
  if (/^(hi|hello|hey)\b/i.test(e.text) && !e.self) {
    var n = store.get("hello:" + e.nick, 0) + 1;
    store.set("hello:" + e.nick, n);
    if (n === 1) reply(prefix + "Welcome, " + e.nick + "!");
  }
});
`

func writeExamplePlugin(dir string) {
	p := filepath.Join(dir, "_example.js")
	if b, err := os.ReadFile(p); os.IsNotExist(err) || (err == nil && !strings.Contains(string(b), "docs/PLUGINS.md")) {
		os.WriteFile(p, []byte(examplePlugin), 0o644)
	}
}

// jsMenuItems lists the JS plugins' menu items for one kind of menu.
func (p *Plugins) jsMenuItems(kind string) []MenuItem {
	p.mu.Lock()
	scripts := append([]*script(nil), p.scripts...)
	p.mu.Unlock()
	var out []MenuItem
	for _, s := range scripts {
		for i, m := range s.menus {
			if !containsStr(m.kinds, kind) || len(m.path) == 0 {
				continue
			}
			level := &out
			for _, dir := range m.path[:len(m.path)-1] {
				found := -1
				for k, it := range *level {
					if it.ID == "" && !it.Sep && it.Label == dir {
						found = k
					}
				}
				if found < 0 {
					*level = append(*level, MenuItem{Label: dir})
					found = len(*level) - 1
				}
				level = &(*level)[found].Items
			}
			*level = append(*level, MenuItem{ID: "js:" + s.name + ":" + strconv.Itoa(i), Label: m.path[len(m.path)-1]})
		}
	}
	return out
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// runJSMenu runs a JS menu item; ref is "<script>:<index>".
func (p *Plugins) runJSMenu(c *Conn, mc MenuContext, ref string) {
	i := strings.LastIndexByte(ref, ':')
	if i < 0 {
		return
	}
	name := ref[:i]
	n, err := strconv.Atoi(ref[i+1:])
	if err != nil {
		return
	}
	p.mu.Lock()
	scripts := append([]*script(nil), p.scripts...)
	p.mu.Unlock()
	for _, s := range scripts {
		if s.name != name || n < 0 || n >= len(s.menus) {
			continue
		}
		e := map[string]any{
			"menu": mc.Kind, "nick": mc.Nick, "channel": mc.Buffer, "target": mc.Buffer, "text": mc.Text,
			"server": c.cfg.Name, "serverId": c.cfg.ID, "me": c.Nick(), "isChannel": c.isChannel(mc.Buffer),
		}
		target := mc.Buffer
		if target == "" && mc.Kind == "nicklist" {
			target = mc.Nick
		}
		s.enqueue(job{fn: s.menus[n].fn, args: []any{e}, conn: c, target: target})
		return
	}
}
