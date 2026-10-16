package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ServerConfig is one saved IRC network.
type ServerConfig struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Host          string   `json:"host"`
	Port          int      `json:"port"`
	TLS           bool     `json:"tls"`
	TLSSkipVerify bool     `json:"tlsSkipVerify"`
	Password      string   `json:"password"` // server PASS
	Nick          string   `json:"nick"`
	AltNick       string   `json:"altNick"`
	Username      string   `json:"username"`
	Realname      string   `json:"realname"`
	AuthMethod    string   `json:"authMethod"` // none | sasl | nickserv
	AuthUser      string   `json:"authUser"`
	AuthPass      string   `json:"authPass"`
	AutoConnect   bool     `json:"autoConnect"`
	AutoJoin      []string `json:"autoJoin"`         // "#chan" or "#chan key"
	RememberChans bool     `json:"rememberChannels"` // keep AutoJoin in sync with joins/parts
	Perform       []string `json:"perform"`          // commands run after connect, e.g. "/mode $me +x"
}

// URLCommand is a trigger (like !solar) that fetches a URL and replies with the result.
type URLCommand struct {
	Trigger   string `json:"trigger"`
	URL       string `json:"url"`
	Template  string `json:"template"`  // optional Go text/template applied to JSON responses
	MaxLines  int    `json:"maxLines"`  // for text responses
	OnlyMe    bool   `json:"onlyMe"`    // only respond when I type the trigger
	Cooldown  int    `json:"cooldown"`  // seconds
	Enabled   bool   `json:"enabled"`
}

type PluginSettings struct {
	BotPrefix       string       `json:"botPrefix"`
	WeatherEnabled  bool         `json:"weatherEnabled"`
	WeatherChannels []string     `json:"weatherChannels"` // empty = all channels
	WeatherCooldown int          `json:"weatherCooldown"`
	WeatherFromMe   bool         `json:"weatherFromMe"`
	WeatherFromOthers bool       `json:"weatherFromOthers"`
	URLCommands     []URLCommand `json:"urlCommands"`
	ScriptsEnabled  bool         `json:"scriptsEnabled"`
}

type Settings struct {
	Theme          string   `json:"theme"`      // auto | light | dark | <custom theme name>
	LightTheme     string   `json:"lightTheme"` // used when theme = auto
	DarkTheme      string   `json:"darkTheme"`
	HighlightWords []string `json:"highlightWords"`
	Notifications  bool     `json:"notifications"`
	NotifyPMs      bool     `json:"notifyPMs"`
	InlineImages   bool     `json:"inlineImages"`
	InlineYouTube  bool     `json:"inlineYouTube"`
	ShowJoinsParts bool     `json:"showJoinsParts"`
	TimeFormat     string   `json:"timeFormat"` // 24h | 12h
	ShowSeconds    bool     `json:"showSeconds"`
	FontSize       int      `json:"fontSize"`
	FontFamily     string   `json:"fontFamily"`
	Logging        bool     `json:"logging"`
	HistoryLines   int      `json:"historyLines"`
	ShowUserList   bool     `json:"showUserList"`
	CompactMode    bool     `json:"compactMode"`
	QuitMessage    string   `json:"quitMessage"`
	Ignore         []string `json:"ignore"`
}

// Shortcut replaces a typed word such as "$shrug" with Text when a message is sent.
type Shortcut struct {
	Trigger string `json:"trigger"`
	Text    string `json:"text"`
}

func defaultShortcuts() []Shortcut {
	return []Shortcut{
		{"$whatever", `¯\_(ツ)_/¯`},
		{"$shrug", `¯\_(ツ)_/¯`},
		{"$tableflip", "(╯°□°)╯︵ ┻━┻"},
		{"$unflip", "┬─┬ノ( º _ ºノ)"},
		{"$lenny", "( ͡° ͜ʖ ͡°)"},
		{"$disapprove", "ಠ_ಠ"},
		{"$sparkles", "✨"},
	}
}

type Config struct {
	Servers    []ServerConfig    `json:"servers"`
	Shortcuts  []Shortcut        `json:"shortcuts"`
	Settings   Settings          `json:"settings"`
	NickColors map[string]string `json:"nickColors"`
	Plugins    PluginSettings    `json:"plugins"`
}

type Store struct {
	mu   sync.Mutex
	dir  string
	path string
	cfg  Config
}

func appDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "IRChatterbox")
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func defaultConfig() Config {
	return Config{
		Servers: []ServerConfig{{
			ID: newID(), Name: "Libera.Chat", Host: "irc.libera.chat", Port: 6697, TLS: true,
			Nick: "", AuthMethod: "none", AutoJoin: []string{}, RememberChans: true, Perform: []string{},
		}},
		Settings: Settings{
			Theme: "auto", LightTheme: "Light", DarkTheme: "Dark",
			HighlightWords: []string{}, Notifications: true, NotifyPMs: true,
			InlineImages: true, InlineYouTube: true, ShowJoinsParts: true,
			TimeFormat: "24h", FontSize: 14, Logging: true, HistoryLines: 200,
			ShowUserList: true, QuitMessage: "IRChatterbox", Ignore: []string{},
		},
		NickColors: map[string]string{},
		Shortcuts:  defaultShortcuts(),
		Plugins: PluginSettings{
			BotPrefix: "[Bot] ", WeatherEnabled: true, WeatherCooldown: 5,
			WeatherFromMe: true, WeatherFromOthers: true, WeatherChannels: []string{},
			URLCommands: []URLCommand{{
				Trigger: "!solar", URL: "", MaxLines: 3, Cooldown: 30, Enabled: true, OnlyMe: true,
			}},
			ScriptsEnabled: true,
		},
	}
}

func NewStore() *Store {
	dir := appDir()
	os.MkdirAll(filepath.Join(dir, "themes"), 0o755)
	os.MkdirAll(filepath.Join(dir, "plugins"), 0o755)
	os.MkdirAll(filepath.Join(dir, "logs"), 0o755)
	s := &Store{dir: dir, path: filepath.Join(dir, "config.json")}
	s.cfg = defaultConfig()
	if b, err := os.ReadFile(s.path); err == nil {
		var c Config
		c = defaultConfig()
		c.Servers = nil
		if json.Unmarshal(b, &c) == nil {
			s.cfg = c
		}
	} else {
		s.saveLocked()
	}
	if s.cfg.NickColors == nil {
		s.cfg.NickColors = map[string]string{}
	}
	for i := range s.cfg.Servers {
		if s.cfg.Servers[i].ID == "" {
			s.cfg.Servers[i].ID = newID()
		}
	}
	writeExamplePlugin(filepath.Join(dir, "plugins"))
	return s
}

func (s *Store) saveLocked() error {
	b, _ := json.MarshalIndent(s.cfg, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.cfg)
	var c Config
	json.Unmarshal(b, &c)
	return c
}

func (s *Store) Update(fn func(c *Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
	return s.saveLocked()
}

func (s *Store) Server(id string) (ServerConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sv := range s.cfg.Servers {
		if sv.ID == id {
			return sv, true
		}
	}
	return ServerConfig{}, false
}

// rememberChannel adds/removes a channel in a server's autojoin list.
func (s *Store) rememberChannel(id, channel string, add bool) {
	s.Update(func(c *Config) {
		for i := range c.Servers {
			sv := &c.Servers[i]
			if sv.ID != id || !sv.RememberChans {
				continue
			}
			idx := -1
			for j, a := range sv.AutoJoin {
				name := strings.Fields(a)
				if len(name) > 0 && strings.EqualFold(name[0], channel) {
					idx = j
				}
			}
			if add && idx < 0 {
				sv.AutoJoin = append(sv.AutoJoin, channel)
			} else if !add && idx >= 0 {
				sv.AutoJoin = append(sv.AutoJoin[:idx], sv.AutoJoin[idx+1:]...)
			}
		}
	})
}

// ---- Themes ----

type Theme struct {
	Name string            `json:"name"`
	Base string            `json:"base"` // light | dark
	Vars map[string]string `json:"vars"`
}

func (s *Store) Themes() []Theme {
	out := []Theme{}
	files, _ := filepath.Glob(filepath.Join(s.dir, "themes", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var t Theme
		if json.Unmarshal(b, &t) == nil && t.Name != "" {
			out = append(out, t)
		}
	}
	return out
}

func safeName(n string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return strings.TrimSpace(r.Replace(n))
}

func (s *Store) SaveTheme(t Theme) error {
	b, _ := json.MarshalIndent(t, "", "  ")
	return os.WriteFile(filepath.Join(s.dir, "themes", safeName(t.Name)+".json"), b, 0o644)
}

func (s *Store) DeleteTheme(name string) error {
	return os.Remove(filepath.Join(s.dir, "themes", safeName(name)+".json"))
}
