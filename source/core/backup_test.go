package core

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func newBackupApp(t *testing.T) *App {
	t.Helper()
	os.Setenv("XDG_CONFIG_HOME", t.TempDir())
	os.Setenv("APPDATA", os.Getenv("XDG_CONFIG_HOME"))
	return NewApp()
}

func TestBackupRoundTrip(t *testing.T) {
	src := newBackupApp(t)
	sv := src.SaveServer(ServerConfig{Name: "Neowin", Host: "chatter.example", Port: 6697, TLS: true, Nick: "andrew",
		Password: "srvpass", AuthMethod: "sasl", AuthPass: "saslpass", Bouncer: "soju", BouncerUser: "andrew", BouncerPass: "bncpass"})
	src.store.Update(func(c *Config) {
		c.Settings.LogFolder = `C:\logs`
		c.Settings.SoundPM = `file:C:\ding.wav`
		c.Shortcuts = []Shortcut{{"$hi", "hello"}}
	})
	src.store.SaveTheme(Theme{Name: "Mine", Base: "dark", Vars: map[string]string{"--bg": "#000"}})

	// without passwords: none of them may appear anywhere in the file
	plain := src.ExportSettings(false, "")
	if plain.Error != "" {
		t.Fatal(plain.Error)
	}
	for _, pw := range []string{"srvpass", "saslpass", "bncpass", `C:\\logs`, "ding.wav"} {
		if strings.Contains(plain.Data, pw) {
			t.Fatalf("export without passwords contains %q", pw)
		}
	}

	// with passwords: encrypted, so still not in the file as text
	if r := src.ExportSettings(true, "short"); r.Error == "" {
		t.Fatal("a short passphrase was accepted")
	}
	enc := src.ExportSettings(true, "correct horse battery")
	if enc.Error != "" {
		t.Fatal(enc.Error)
	}
	for _, pw := range []string{"srvpass", "saslpass", "bncpass"} {
		if strings.Contains(enc.Data, pw) {
			t.Fatalf("encrypted export contains %q in the clear", pw)
		}
	}

	dst := newBackupApp(t)
	info := dst.InspectSettings(enc.Data)
	if info.Error != "" || !info.HasPasswords || info.Servers != 1 || info.Themes != 1 {
		t.Fatalf("inspect: %+v", info)
	}
	if e := dst.ImportSettings(enc.Data, "wrong passphrase"); e != errWrongBackupPw {
		t.Fatalf("wrong passphrase: got %q", e)
	}
	if e := dst.ImportSettings(enc.Data, "correct horse battery"); e != "" {
		t.Fatal(e)
	}
	got, ok := dst.store.Server(sv.ID)
	if !ok || got.Password != "srvpass" || got.AuthPass != "saslpass" || got.BouncerPass != "bncpass" || got.Host != "chatter.example" {
		t.Fatalf("server after import: %+v", got)
	}
	cfg := dst.store.Get()
	if len(cfg.Shortcuts) != 1 || cfg.Shortcuts[0].Trigger != "$hi" {
		t.Fatalf("shortcuts: %+v", cfg.Shortcuts)
	}
	if cfg.Settings.LogFolder != "" || cfg.Settings.SoundPM != "" {
		t.Fatalf("machine paths imported: %q %q", cfg.Settings.LogFolder, cfg.Settings.SoundPM)
	}
	if len(dst.store.Themes()) != 1 {
		t.Fatal("theme not imported")
	}

	// importing the password-less file again keeps the passwords already saved
	if e := dst.ImportSettings(plain.Data, ""); e != "" {
		t.Fatal(e)
	}
	if got, _ := dst.store.Server(sv.ID); got.BouncerPass != "bncpass" {
		t.Fatal("password-less import wiped the saved password")
	}

	// junk and newer formats are refused
	if dst.InspectSettings(`{"app":"other"}`).Error == "" {
		t.Fatal("accepted a non-backup")
	}
	var f map[string]any
	json.Unmarshal([]byte(plain.Data), &f)
	f["format"] = 99
	b, _ := json.Marshal(f)
	if dst.InspectSettings(string(b)).Error == "" {
		t.Fatal("accepted a newer format")
	}
}

// A backup from the web client has no servers, no plugins and only the settings the browser
// knows: importing it must not switch off the rest.
func TestImportPartialBackup(t *testing.T) {
	a := newBackupApp(t)
	a.store.Update(func(c *Config) {
		c.Settings.TrayIcon, c.Settings.AutoUpdateCheck = true, true
		c.Plugins.WeatherEnabled, c.Plugins.BotPrefix = true, "[Bot] "
	})
	web := `{"app":"IRChatterbox","format":1,"version":"web","servers":[],
		"settings":{"timeFormat":"12h","quitMessage":"from the web"},
		"shortcuts":[{"key":"$hi","text":"hello"}],"nickColors":{"alice":"#ff0000"},"themes":[]}`
	if e := a.ImportSettings(web, ""); e != "" {
		t.Fatal(e)
	}
	c := a.store.Get()
	if c.Settings.TimeFormat != "12h" || c.Settings.QuitMessage != "from the web" {
		t.Fatalf("settings in the file weren't applied: %+v", c.Settings)
	}
	if !c.Settings.TrayIcon || !c.Settings.AutoUpdateCheck {
		t.Fatal("settings missing from the file were reset")
	}
	if !c.Plugins.WeatherEnabled || c.Plugins.BotPrefix != "[Bot] " {
		t.Fatalf("plugins were reset by a backup without plugins: %+v", c.Plugins)
	}
	if c.NickColors["alice"] != "#ff0000" || len(c.Shortcuts) != 1 {
		t.Fatal("nick colours or shortcuts weren't imported")
	}
}
