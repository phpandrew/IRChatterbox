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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

)

type Plugins struct {
	app      *App
	mu       sync.Mutex
	cooldown map[string]time.Time
	scripts  []*script
}

// OnJoin is kept for the IRC layer; it's just a "join" plugin event.
func (p *Plugins) OnJoin(c *Conn, channel, nick string, self bool) {
	p.Emit("join", c, channel, map[string]any{"channel": channel, "nick": nick, "self": self, "isChannel": true})
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
	p.Emit("message", c, replyTo, map[string]any{
		"channel": replyTo, "nick": nick, "text": plain, "raw": text, "self": self,
		"isChannel": c.isChannel(replyTo), "isPM": !c.isChannel(replyTo),
	})
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

