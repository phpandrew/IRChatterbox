package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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
	Encoding      string   `json:"encoding"`         // "" / auto = UTF-8 with Windows-1252 fallback, or e.g. windows-1251, iso-8859-1
	ChannelOrder  []string `json:"channelOrder"`     // sidebar order (lowercase names), set by dragging

	// Bouncer: "" connects straight to the network; "soju" or "znc" connects through a bouncer
	// at Host:Port, which stays online and replays what you missed.
	Bouncer        string `json:"bouncer"`
	BouncerUser    string `json:"bouncerUser"`
	BouncerPass    string `json:"bouncerPass"`
	BouncerNetwork string `json:"bouncerNetwork"` // which of the bouncer's networks (optional)
}

// bouncerLogin is the bouncer username with the network appended (andrew/libera),
// the form both soju and ZNC accept.
func (s ServerConfig) bouncerLogin() string {
	u := s.BouncerUser
	if u == "" {
		u = s.Nick
	}
	if s.BouncerNetwork != "" {
		u += "/" + s.BouncerNetwork
	}
	return u
}

// URLCommand is a trigger (like !solar) that fetches a URL and replies with the result.
type URLCommand struct {
	Trigger  string `json:"trigger"`
	URL      string `json:"url"`
	Template string `json:"template"` // optional Go text/template applied to JSON responses
	MaxLines int    `json:"maxLines"` // for text responses
	OnlyMe   bool   `json:"onlyMe"`   // only respond when I type the trigger
	Cooldown int    `json:"cooldown"` // seconds
	Enabled  bool   `json:"enabled"`
}

// MusicSettings controls the !music now-playing command.
type MusicSettings struct {
	Enabled  bool   `json:"enabled"`
	Trigger  string `json:"trigger"`
	Source   string `json:"source"` // auto | smtc | window | file
	File     string `json:"file"`
	Format   string `json:"format"`   // reply for the trigger
	NPFormat string `json:"npFormat"` // /np action text
	OnlyMe   bool   `json:"onlyMe"`
	Cooldown int    `json:"cooldown"`
}

type PluginSettings struct {
	Music             MusicSettings `json:"music"`
	BotPrefix         string        `json:"botPrefix"`
	WeatherEnabled    bool          `json:"weatherEnabled"`
	WeatherChannels   []string      `json:"weatherChannels"` // empty = all channels
	WeatherCooldown   int           `json:"weatherCooldown"`
	WeatherFromMe     bool          `json:"weatherFromMe"`
	WeatherFromOthers bool          `json:"weatherFromOthers"`
	URLCommands       []URLCommand  `json:"urlCommands"`
	ScriptsEnabled    bool          `json:"scriptsEnabled"`
}

type Settings struct {
	Theme            string   `json:"theme"`      // auto | light | dark | <custom theme name>
	LightTheme       string   `json:"lightTheme"` // used when theme = auto
	DarkTheme        string   `json:"darkTheme"`
	HighlightWords   []string `json:"highlightWords"`
	NotifyList       []string `json:"notifyList"`    // friends to watch with MONITOR
	NotifyFriends    bool     `json:"notifyFriends"` // toast when a friend comes online
	SendTyping       bool     `json:"sendTyping"`    // tell others when you're typing (+typing)
	Notifications    bool     `json:"notifications"`
	NotifyPMs        bool     `json:"notifyPMs"`
	InlineImages     bool     `json:"inlineImages"`
	InlineYouTube    bool     `json:"inlineYouTube"`
	InlineVideos     bool     `json:"inlineVideos"` // video/audio files, imgur gifv, Giphy, Vimeo, Streamable
	ShowJoinsParts   bool     `json:"showJoinsParts"`
	TimeFormat       string   `json:"timeFormat"` // 24h | 12h
	ShowSeconds      bool     `json:"showSeconds"`
	FontSize         int      `json:"fontSize"`
	FontFamily       string   `json:"fontFamily"`
	Logging          bool     `json:"logging"`
	HistoryLines     int      `json:"historyLines"`
	TextLogs         bool     `json:"textLogs"`    // also write readable .txt logs
	LogFolder        string   `json:"logFolder"`   // where .txt logs go (default: data folder\\logs)
	LogMaxLines      int      `json:"logMaxLines"` // keep at most this many lines per log file; 0 = unlimited
	ShowUserList     bool     `json:"showUserList"`
	CompactMode      bool     `json:"compactMode"`
	QuitMessage      string   `json:"quitMessage"`
	StartWithWindows bool     `json:"startWithWindows"`
	StartMinimized   bool     `json:"startMinimized"`
	ConfirmOnClose   bool     `json:"confirmOnClose"`
	MarkerLine       bool     `json:"markerLine"`
	NickContrast     bool     `json:"nickContrast"` // flip custom nick colours that are unreadable on the current theme
	Ignore           []string `json:"ignore"`
	FilterWords      []string `json:"filterWords"` // masked as ••• wherever they appear (Settings → Highlights)

	// updates
	AutoUpdateCheck bool   `json:"autoUpdateCheck"`
	SkippedVersion  string `json:"skippedVersion"`

	// tray & attention
	TrayIcon    bool `json:"trayIcon"`
	FlashOnPing bool `json:"flashOnPing"` // flash tray + taskbar when highlighted / PM'd
	CloseToTray bool `json:"closeToTray"` // closing the window hides it to the tray

	// sending
	PasteWarnLines int `json:"pasteWarnLines"` // confirm before sending this many lines or more; 0 = never

	// auto-away
	AutoAway        bool   `json:"autoAway"`
	AutoAwayMinutes int    `json:"autoAwayMinutes"`
	AutoAwayMessage string `json:"autoAwayMessage"`

	// sounds: "" (none) | chime | ping | pop | bell | blip | file:<path>
	SoundHighlight string `json:"soundHighlight"`
	SoundPM        string `json:"soundPM"`
	SoundVolume    int    `json:"soundVolume"` // 0-100

	// image uploads (attach button, paste, drag and drop): "irchatterbox" | "imgur"
	UploadHost    string `json:"uploadHost"`
	ImgurClientID string `json:"imgurClientId"`
	UploadURL     string `json:"uploadURL"` // the custom server's upload address

	// Android: keep connections live while the app is in the background (a foreground
	// service with a status-bar notification). Off: disconnect in the background and let
	// the bouncer replay what you missed when you come back.
	KeepAlive bool `json:"keepAlive"`
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

// WindowState is the main window's last size/position, restored on launch.
type WindowState struct {
	X          int  `json:"x"`
	Y          int  `json:"y"`
	Width      int  `json:"width"`
	Height     int  `json:"height"`
	Maximized  bool `json:"maximized"`
	Fullscreen bool `json:"fullscreen"`
	Saved      bool `json:"saved"`
}

// StartBuffer is the channel shown when IRChatterbox opens.
type StartBuffer struct {
	Server string `json:"server"`
	Buffer string `json:"buffer"`
}

type Config struct {
	Window     WindowState       `json:"window"`
	StartView  StartBuffer       `json:"startView"`
	Servers    []ServerConfig    `json:"servers"`
	Shortcuts  []Shortcut        `json:"shortcuts"`
	Settings   Settings          `json:"settings"`
	NickColors map[string]string `json:"nickColors"`
	Plugins    PluginSettings    `json:"plugins"`
	// InstallID is a random ID made once per install. Reports and uploads send it so the
	// servers can rate-limit per install; it says nothing about who you are.
	InstallID string `json:"installId"`
}

type Store struct {
	mu   sync.Mutex
	dir  string
	path string
	cfg  Config
}

// dataDirOverride, when set (Android), is used instead of %APPDATA%\IRChatterbox.
var dataDirOverride string

func appDir() string {
	if dataDirOverride != "" {
		return dataDirOverride
	}
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
		Servers: []ServerConfig{},
		Settings: Settings{
			Theme: "auto", LightTheme: "Light", DarkTheme: "Dark",
			HighlightWords: []string{}, Notifications: true, NotifyPMs: true, NotifyList: []string{}, NotifyFriends: true, SendTyping: true,
			InlineImages: true, InlineYouTube: true, InlineVideos: true, ShowJoinsParts: true,
			TimeFormat: "24h", FontSize: 14, Logging: true, HistoryLines: 200, LogMaxLines: 10000,
			ShowUserList: true, QuitMessage: "IRChatterbox", ConfirmOnClose: true, MarkerLine: true, NickContrast: true, Ignore: []string{},
			AutoUpdateCheck: true, TrayIcon: true, FlashOnPing: true, PasteWarnLines: 5,
			AutoAwayMinutes: 15, AutoAwayMessage: "Away from keyboard",
			SoundHighlight: "chime", SoundPM: "ping", SoundVolume: 70,
			KeepAlive: true, UploadHost: UploadHostIRChatterbox,
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
			Music: MusicSettings{
				Enabled: true, Trigger: "!music", Source: "auto", Cooldown: 10,
				Format: "Now playing: {np}", NPFormat: "is listening to {np}",
			},
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
	if len(s.cfg.InstallID) != 32 {
		b := make([]byte, 16)
		rand.Read(b)
		s.cfg.InstallID = hex.EncodeToString(b)
		s.saveLocked()
	}
	if s.cfg.NickColors == nil {
		s.cfg.NickColors = map[string]string{}
	}
	// Older versions created an empty Libera.Chat placeholder with no nick, which made the
	// setup popup appear on every launch. Drop it if it was never filled in.
	kept := []ServerConfig{}
	for _, sv := range s.cfg.Servers {
		if sv.Nick == "" && sv.Host == "irc.libera.chat" && sv.AuthPass == "" && len(sv.AutoJoin) == 0 && len(sv.Perform) == 0 {
			continue
		}
		if sv.ID == "" {
			sv.ID = newID()
		}
		kept = append(kept, sv)
	}
	if len(kept) != len(s.cfg.Servers) {
		s.cfg.Servers = kept
		s.saveLocked()
	}
	writeExamplePlugin(filepath.Join(dir, "plugins"))
	writeExampleMirc(filepath.Join(dir, "plugins"))
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

// setAutoJoin adds or removes a channel from a server's auto-join list (regardless of
// the "remember channels" option).
func (s *Store) setAutoJoin(id, channel string, on bool) {
	s.Update(func(c *Config) {
		for i := range c.Servers {
			sv := &c.Servers[i]
			if sv.ID != id {
				continue
			}
			idx := -1
			for j, a := range sv.AutoJoin {
				if f := strings.Fields(a); len(f) > 0 && strings.EqualFold(f[0], channel) {
					idx = j
				}
			}
			if on && idx < 0 {
				sv.AutoJoin = append(sv.AutoJoin, channel)
			} else if !on && idx >= 0 {
				sv.AutoJoin = append(sv.AutoJoin[:idx], sv.AutoJoin[idx+1:]...)
			}
		}
	})
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

// ReorderServers saves the sidebar order of servers (dragged in the UI).
func (s *Store) reorderServers(ids []string) {
	s.Update(func(c *Config) {
		pos := map[string]int{}
		for i, id := range ids {
			pos[id] = i
		}
		sort.SliceStable(c.Servers, func(i, j int) bool {
			pi, oki := pos[c.Servers[i].ID]
			pj, okj := pos[c.Servers[j].ID]
			if !oki {
				pi = len(ids) + i
			}
			if !okj {
				pj = len(ids) + j
			}
			return pi < pj
		})
	})
}

// setChannelOrder saves a server's channel order and puts its auto-join list in the same
// order, so channels are also joined in that order.
func (s *Store) setChannelOrder(id string, order []string) {
	s.Update(func(c *Config) {
		for i := range c.Servers {
			sv := &c.Servers[i]
			if sv.ID != id {
				continue
			}
			sv.ChannelOrder = nil
			pos := map[string]int{}
			for i, ch := range order {
				l := strings.ToLower(ch)
				sv.ChannelOrder = append(sv.ChannelOrder, l)
				pos[l] = i
			}
			sort.SliceStable(sv.AutoJoin, func(a, b int) bool {
				na, nb := strings.ToLower(strings.Fields(sv.AutoJoin[a] + " ")[0]), strings.ToLower(strings.Fields(sv.AutoJoin[b] + " ")[0])
				pa, oka := pos[na]
				pb, okb := pos[nb]
				if !oka {
					pa = 1 << 30
				}
				if !okb {
					pb = 1 << 30
				}
				return pa < pb
			})
		}
	})
}
