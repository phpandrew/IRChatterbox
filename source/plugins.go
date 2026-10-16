package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/dop251/goja"
)

type Plugins struct {
	app      *App
	mu       sync.Mutex
	cooldown map[string]time.Time
	scripts  []*script
}

func NewPlugins(a *App) *Plugins {
	return &Plugins{app: a, cooldown: map[string]time.Time{}}
}

func (p *Plugins) ready(key string, secs int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.cooldown[key]; ok && time.Since(t) < time.Duration(secs)*time.Second {
		return false
	}
	p.cooldown[key] = time.Now()
	return true
}

// OnMessage is called for every PRIVMSG in a channel or query, including ones I send.
// replyTo is the channel, or the other person's nick in a query.
func (p *Plugins) OnMessage(c *Conn, replyTo, nick, text string, self bool) {
	cfg := p.app.store.Get().Plugins
	plain := strings.TrimSpace(stripFormatting(text))
	if cfg.BotPrefix != "" && strings.HasPrefix(plain, strings.TrimSpace(cfg.BotPrefix)) {
		return // never react to bot output (mine or anyone else's)
	}

	// --- weather conversion ---
	if cfg.WeatherEnabled && ((self && cfg.WeatherFromMe) || (!self && cfg.WeatherFromOthers)) && channelAllowed(cfg.WeatherChannels, replyTo) {
		if out := convertTemps(plain); out != "" {
			if p.ready(c.cfg.ID+"|weather|"+lower(replyTo), cfg.WeatherCooldown) {
				go func() {
					time.Sleep(150 * time.Millisecond) // keep it after the triggering line
					c.Say(replyTo, cfg.BotPrefix+out, true)
				}()
			}
		}
	}

	word, args, _ := strings.Cut(plain, " ")

	// --- now playing (!music) ---
	if m := cfg.Music; m.Enabled && m.Trigger != "" && strings.EqualFold(word, m.Trigger) && (self || !m.OnlyMe) {
		if p.ready(c.cfg.ID+"|music|"+lower(replyTo), m.Cooldown) {
			go func() {
				np, err := getNowPlaying(m)
				if err != nil {
					if self {
						c.info(replyTo, "error", m.Trigger+": "+err.Error())
					} else {
						c.Say(replyTo, cfg.BotPrefix+"Nothing playing right now", true)
					}
					return
				}
				c.Say(replyTo, cfg.BotPrefix+np.Format(m.Format), true)
			}()
		}
	}

	for _, uc := range cfg.URLCommands {
		if !uc.Enabled || uc.URL == "" || !strings.EqualFold(word, uc.Trigger) {
			continue
		}
		if uc.OnlyMe && !self {
			continue
		}
		if !p.ready(c.cfg.ID+"|url|"+lower(uc.Trigger)+"|"+lower(replyTo), uc.Cooldown) {
			continue
		}
		uc := uc
		uc.URL = strings.ReplaceAll(uc.URL, "{args}", url.QueryEscape(strings.TrimSpace(args)))
		go func() {
			lines, err := fetchURLCommand(uc)
			if err != nil {
				c.info(replyTo, "error", uc.Trigger+": "+err.Error())
				return
			}
			for _, l := range lines {
				c.Say(replyTo, cfg.BotPrefix+l, true)
			}
		}()
	}

	// --- JS scripts ---
	if cfg.ScriptsEnabled {
		p.dispatch("message", c, map[string]any{
			"server": c.cfg.Name, "target": replyTo, "channel": replyTo, "nick": nick, "text": plain, "raw": text, "self": self,
			"isChannel": c.isChannel(replyTo),
		})
	}
}

func (p *Plugins) OnJoin(c *Conn, channel, nick string, self bool) {
	if p.app.store.Get().Plugins.ScriptsEnabled {
		p.dispatch("join", c, map[string]any{"server": c.cfg.Name, "target": channel, "channel": channel, "nick": nick, "self": self, "isChannel": true})
	}
}

func channelAllowed(list []string, ch string) bool {
	if len(list) == 0 {
		return true
	}
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), ch) {
			return true
		}
	}
	return false
}

// ---------- weather ----------

var urlRe = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)
var tempRe = regexp.MustCompile(`(?i)(^|[^\w.\-+])([-+−]?\d{1,3}(?:[.,]\d+)?)\s?(°\s?)?(celsius|fahrenheit|c|f)\b`)

// fmtNum shows up to one decimal place, dropping a trailing ".0" (64.4, 68, -15.8).
func fmtNum(v float64) string {
	v = math.Round(v*10) / 10
	if v == 0 {
		v = 0 // avoid "-0"
	}
	return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0")
}

// convertTemps finds temperatures like "18c", "-5 °C", "68F" and returns "18 C = 64.4 F".
func convertTemps(text string) string {
	text = urlRe.ReplaceAllString(text, " ")
	var parts []string
	seen := map[string]bool{}
	for _, m := range tempRe.FindAllStringSubmatch(text, 6) {
		numStr := strings.ReplaceAll(strings.ReplaceAll(m[2], "−", "-"), ",", ".")
		v, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			continue
		}
		unit := strings.ToUpper(m[4][:1])
		var out string
		if unit == "C" {
			if v < -90 || v > 60 {
				continue
			}
			out = fmtNum(v*9/5+32) + " F"
		} else {
			if v < -130 || v > 140 {
				continue
			}
			out = fmtNum((v-32)*5/9) + " C"
		}
		orig := fmtNum(v) + " " + unit
		line := orig + " = " + out
		if !seen[line] {
			seen[line] = true
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " | ")
}

// ---------- URL commands ----------

var tagRe = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]+>`)
var httpClient = &http.Client{Timeout: 12 * time.Second}

func fetchURLCommand(uc URLCommand) ([]string, error) {
	if uc.URL == "" {
		return nil, fmt.Errorf("no URL set — configure it in Settings → Plugins")
	}
	req, err := http.NewRequest("GET", uc.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "IRChatterbox/"+appVersion)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	maxLines := uc.MaxLines
	if maxLines <= 0 {
		maxLines = 3
	}
	trim := bytes.TrimSpace(body)
	var data any
	isJSON := len(trim) > 0 && (trim[0] == '{' || trim[0] == '[') && json.Unmarshal(trim, &data) == nil

	var text string
	switch {
	case isJSON && strings.TrimSpace(uc.Template) != "":
		t, err := template.New("t").Option("missingkey=zero").Funcs(template.FuncMap{
			"round": func(v any, places int) string {
				f, _ := toFloat(v)
				return strconv.FormatFloat(f, 'f', places, 64)
			},
			"kw": func(v any) string { // watts -> kW
				f, _ := toFloat(v)
				return strconv.FormatFloat(f/1000, 'f', 2, 64)
			},
		}).Parse(uc.Template)
		if err != nil {
			return nil, fmt.Errorf("template: %v", err)
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("template: %v", err)
		}
		text = buf.String()
	case isJSON:
		text = flattenJSON(data)
	default:
		text = string(body)
		if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") || bytes.Contains(trim[:min(len(trim), 200)], []byte("<")) {
			text = html.UnescapeString(tagRe.ReplaceAllString(text, "\n"))
		}
	}
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.Join(strings.Fields(l), " ")
		if l != "" {
			lines = append(lines, l)
		}
		if len(lines) >= maxLines {
			break
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return lines, nil
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

// flattenJSON renders simple objects as "key: value | key: value".
func flattenJSON(v any) string {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			switch val := x[k].(type) {
			case map[string]any, []any:
				continue
			default:
				parts = append(parts, fmt.Sprintf("%s: %v", k, val))
			}
		}
		return strings.Join(parts, " | ")
	case []any:
		var lines []string
		for _, e := range x {
			lines = append(lines, flattenJSON(e))
		}
		return strings.Join(lines, "\n")
	default:
		return fmt.Sprint(v)
	}
}

// ---------- JavaScript plugins ----------

type script struct {
	name     string
	mu       sync.Mutex
	vm       *goja.Runtime
	handlers map[string][]goja.Callable
	cur      *Conn
	curTgt   string
}

func (p *Plugins) LoadScripts() string {
	dir := filepath.Join(p.app.store.dir, "plugins")
	files, _ := filepath.Glob(filepath.Join(dir, "*.js"))
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
	msg := fmt.Sprintf("Plugins loaded: %d script(s) %s", len(names), strings.Join(names, ", "))
	if len(errs) > 0 {
		msg += " — errors: " + strings.Join(errs, "; ")
	}
	return msg
}

func (p *Plugins) newScript(name, src string) (*script, error) {
	s := &script{name: name, vm: goja.New(), handlers: map[string][]goja.Callable{}}
	vm := s.vm
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	cfg := p.app.store.Get().Plugins
	vm.Set("prefix", cfg.BotPrefix)
	vm.Set("on", func(call goja.FunctionCall) goja.Value {
		ev := call.Argument(0).String()
		if fn, ok := goja.AssertFunction(call.Argument(1)); ok {
			s.handlers[ev] = append(s.handlers[ev], fn)
		}
		return goja.Undefined()
	})
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
	vm.Set("raw", func(line string) {
		if s.cur != nil {
			s.cur.SendRaw(line)
		}
	})
	vm.Set("me", func() string {
		if s.cur != nil {
			return s.cur.Nick()
		}
		return ""
	})
	vm.Set("log", func(text string) {
		if s.cur != nil {
			s.cur.info("", "plugin", "["+name+"] "+text)
		}
	})
	vm.Set("httpGet", func(u string) string {
		resp, err := httpClient.Get(u)
		if err != nil {
			return ""
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		return string(b)
	})
	vm.Set("toF", func(c float64) float64 { return c*9/5 + 32 })
	vm.Set("toC", func(f float64) float64 { return (f - 32) * 5 / 9 })
	if _, err := vm.RunString(src); err != nil {
		return nil, err
	}
	return s, nil
}

func (p *Plugins) dispatch(event string, c *Conn, data map[string]any) {
	p.mu.Lock()
	scripts := append([]*script(nil), p.scripts...)
	p.mu.Unlock()
	for _, s := range scripts {
		if len(s.handlers[event]) == 0 {
			continue
		}
		go func(s *script) {
			s.mu.Lock()
			defer s.mu.Unlock()
			defer func() {
				if r := recover(); r != nil {
					c.info("", "error", fmt.Sprintf("[%s] crashed: %v", s.name, r))
				}
			}()
			s.cur, s.curTgt = c, fmt.Sprint(data["target"])
			t := time.AfterFunc(15*time.Second, func() { s.vm.Interrupt("timeout") })
			defer t.Stop()
			obj := s.vm.ToValue(data)
			for _, fn := range s.handlers[event] {
				if _, err := fn(goja.Undefined(), obj); err != nil {
					c.info("", "error", fmt.Sprintf("[%s] %v", s.name, err))
					s.vm.ClearInterrupt()
				}
			}
		}(s)
	}
}

const examplePlugin = `// IRChatterbox plugin example.
// Files in this folder ending in .js are loaded at startup (or with /reload).
// Files starting with "_" (like this one) are skipped — copy it to e.g. "dice.js" to enable.
//
// API:
//   on("message", fn)  fn(e) with e.server, e.channel, e.nick, e.text, e.self, e.isChannel
//   on("join", fn)     fn(e) with e.channel, e.nick, e.self
//   reply(text)        send to the channel/query the event came from
//   say(target, text)  action(target, text)  raw("IRC LINE")
//   httpGet(url)       returns the body as a string (blocking, 12s timeout)
//   me()               your current nick
//   log(text)          print to the server window
//   prefix             your bot prefix from settings, e.g. "[Bot] "
//   toF(c) / toC(f)    temperature helpers

on("message", function (e) {
  if (e.text === "!roll") {
    reply(prefix + e.nick + " rolled " + (1 + Math.floor(Math.random() * 6)));
  }
});
`

func writeExamplePlugin(dir string) {
	p := filepath.Join(dir, "_example.js")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		os.WriteFile(p, []byte(examplePlugin), 0o644)
	}
}
