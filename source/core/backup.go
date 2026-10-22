package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Settings backup (Settings → About → Back up settings): one JSON file with your servers,
// shortcuts, settings, nick colours, plugins and custom themes, to move to another PC or
// phone or keep as a backup.
//
// Passwords (server, SASL/NickServ, bouncer) are left out unless you choose to include
// them. Then they're encrypted with a passphrase you pick (AES-256-GCM, key from
// PBKDF2-SHA256); the rest of the file stays readable. The window position, install ID
// and paths that only mean something on this computer aren't exported.

const (
	backupFormat     = 1
	backupKDFIter    = 600_000
	backupMinPass    = 8
	backupMaxSize    = 4 << 20
	errWrongBackupPw = "Wrong passphrase, or the file was changed."
)

type backupFile struct {
	App        string            `json:"app"` // "IRChatterbox"
	Format     int               `json:"format"`
	Version    string            `json:"version"`
	Exported   string            `json:"exported"`
	Servers    []ServerConfig    `json:"servers"`
	Shortcuts  []Shortcut        `json:"shortcuts"`
	Settings   Settings          `json:"settings"`
	NickColors map[string]string `json:"nickColors"`
	Plugins    PluginSettings    `json:"plugins"`
	Themes     []Theme           `json:"themes"`
	Secrets    *backupSecrets    `json:"secrets,omitempty"`
}

// backupSecrets is the encrypted map of server ID → passwords.
type backupSecrets struct {
	KDF   string `json:"kdf"` // "pbkdf2-sha256"
	Iter  int    `json:"iter"`
	Salt  []byte `json:"salt"`
	Nonce []byte `json:"nonce"`
	Data  []byte `json:"data"`
}

type serverSecrets struct {
	Password    string `json:"password,omitempty"`
	AuthPass    string `json:"authPass,omitempty"`
	BouncerPass string `json:"bouncerPass,omitempty"`
	ProxyPass   string `json:"proxyPass,omitempty"`
	ClientKey   string `json:"clientKey,omitempty"`
}

func (s serverSecrets) empty() bool { return s == serverSecrets{} }

// BackupResult is what ExportSettings returns: the file's text, or an error.
type BackupResult struct {
	Data  string `json:"data"`
	Name  string `json:"name"` // suggested file name
	Error string `json:"error,omitempty"`
}

// BackupOpen is a backup file read from disk (desktop open dialog).
type BackupOpen struct {
	Data  string `json:"data"`
	Error string `json:"error,omitempty"`
}

// BackupInfo describes a backup before it's imported.
type BackupInfo struct {
	Error        string `json:"error,omitempty"`
	Version      string `json:"version"`
	Exported     string `json:"exported"`
	Servers      int    `json:"servers"`
	Shortcuts    int    `json:"shortcuts"`
	Themes       int    `json:"themes"`
	HasPasswords bool   `json:"hasPasswords"`
}

// ExportSettings builds a backup. With withPasswords, passphrase encrypts the passwords.
func (a *App) ExportSettings(withPasswords bool, passphrase string) BackupResult {
	cfg := a.store.Get()
	f := backupFile{
		App: "IRChatterbox", Format: backupFormat, Version: appVersion,
		Exported:   time.Now().UTC().Format(time.RFC3339),
		Shortcuts:  cfg.Shortcuts,
		Settings:   cfg.Settings,
		NickColors: cfg.NickColors,
		Plugins:    cfg.Plugins,
		Themes:     a.store.Themes(),
	}
	// paths and per-install state stay behind
	f.Settings.LogFolder = ""
	f.Settings.SkippedVersion = ""
	f.Settings.SoundHighlight = portableSound(f.Settings.SoundHighlight)
	f.Settings.SoundPM = portableSound(f.Settings.SoundPM)
	f.Plugins.Music.File = ""

	secrets := map[string]serverSecrets{}
	for _, sv := range cfg.Servers {
		sec := serverSecrets{Password: sv.Password, AuthPass: sv.AuthPass, BouncerPass: sv.BouncerPass, ProxyPass: sv.ProxyPass, ClientKey: sv.ClientKey}
		if !sec.empty() {
			secrets[sv.ID] = sec
		}
		sv.Password, sv.AuthPass, sv.BouncerPass, sv.ProxyPass, sv.ClientKey = "", "", "", "", ""
		sv.SecretsStored = false
		f.Servers = append(f.Servers, sv)
	}
	if withPasswords && len(secrets) > 0 {
		if len([]rune(passphrase)) < backupMinPass {
			return BackupResult{Error: fmt.Sprintf("Choose a passphrase of at least %d characters.", backupMinPass)}
		}
		plain, _ := json.Marshal(secrets)
		enc, err := sealSecrets(plain, passphrase)
		if err != nil {
			return BackupResult{Error: err.Error()}
		}
		f.Secrets = enc
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return BackupResult{Error: err.Error()}
	}
	return BackupResult{Data: string(b), Name: "irchatterbox-settings-" + time.Now().Format("2006-01-02") + ".json"}
}

// InspectSettings reads a backup without changing anything, for the confirm step.
func (a *App) InspectSettings(data string) BackupInfo {
	f, err := parseBackup(data)
	if err != nil {
		return BackupInfo{Error: err.Error()}
	}
	return BackupInfo{
		Version: f.Version, Exported: f.Exported,
		Servers: len(f.Servers), Shortcuts: len(f.Shortcuts), Themes: len(f.Themes),
		HasPasswords: f.Secrets != nil,
	}
}

// ImportSettings applies a backup. Servers are matched by ID: ones in the file replace
// the saved copy (keeping its passwords when the file has none) and new ones are added;
// servers that aren't in the file are left alone. Shortcuts, settings and plugins are
// replaced, nick colours and themes are merged. An empty passphrase skips the passwords.
func (a *App) ImportSettings(data, passphrase string) string {
	f, err := parseBackup(data)
	if err != nil {
		return err.Error()
	}
	secrets := map[string]serverSecrets{}
	if f.Secrets != nil && passphrase != "" {
		plain, err := openSecrets(f.Secrets, passphrase)
		if err != nil {
			return err.Error()
		}
		if err := json.Unmarshal(plain, &secrets); err != nil {
			return errWrongBackupPw
		}
	}

	old := a.store.Get().Settings
	a.store.Update(func(c *Config) {
		byID := map[string]int{}
		for i, sv := range c.Servers {
			byID[sv.ID] = i
		}
		for _, sv := range f.Servers {
			if sv.ID == "" || sv.Host == "" {
				continue
			}
			sec := secrets[sv.ID]
			sv.Password, sv.AuthPass, sv.BouncerPass, sv.ProxyPass, sv.ClientKey = sec.Password, sec.AuthPass, sec.BouncerPass, sec.ProxyPass, sec.ClientKey
			if i, ok := byID[sv.ID]; ok {
				cur := c.Servers[i]
				if sv.Password == "" {
					sv.Password = cur.Password
				}
				if sv.AuthPass == "" {
					sv.AuthPass = cur.AuthPass
				}
				if sv.BouncerPass == "" {
					sv.BouncerPass = cur.BouncerPass
				}
				if sv.ProxyPass == "" {
					sv.ProxyPass = cur.ProxyPass
				}
				if sv.ClientKey == "" && sv.ClientCert == cur.ClientCert {
					sv.ClientKey = cur.ClientKey
				}
				c.Servers[i] = sv
			} else {
				byID[sv.ID] = len(c.Servers)
				c.Servers = append(c.Servers, sv)
			}
		}

		if f.Shortcuts != nil {
			c.Shortcuts = f.Shortcuts
		}

		s := f.Settings
		// keep what belongs to this computer
		s.LogFolder = old.LogFolder
		s.SkippedVersion = old.SkippedVersion
		s.StartWithWindows, s.StartMinimized = old.StartWithWindows, old.StartMinimized
		if s.SoundHighlight == "" && strings.HasPrefix(old.SoundHighlight, "file:") {
			s.SoundHighlight = old.SoundHighlight
		}
		if s.SoundPM == "" && strings.HasPrefix(old.SoundPM, "file:") {
			s.SoundPM = old.SoundPM
		}
		c.Settings = s

		if c.NickColors == nil {
			c.NickColors = map[string]string{}
		}
		for k, v := range f.NickColors {
			c.NickColors[k] = v
		}

		musicFile := c.Plugins.Music.File
		c.Plugins = f.Plugins
		c.Plugins.Music.File = musicFile
	})
	for _, t := range f.Themes {
		if strings.TrimSpace(t.Name) != "" {
			a.store.SaveTheme(t)
		}
	}
	a.updateMonitors(old.NotifyList, f.Settings.NotifyList)
	return ""
}

func parseBackup(data string) (backupFile, error) {
	var f backupFile
	if len(data) > backupMaxSize {
		return f, errors.New("That file is too big to be an IRChatterbox backup.")
	}
	if err := json.Unmarshal([]byte(data), &f); err != nil || f.App != "IRChatterbox" {
		return f, errors.New("That isn't an IRChatterbox settings backup.")
	}
	if f.Format > backupFormat {
		return f, fmt.Errorf("This backup was made by a newer IRChatterbox (%s). Update first.", f.Version)
	}
	return f, nil
}

// portableSound keeps the built-in sounds and drops a file path from this computer.
func portableSound(s string) string {
	if strings.HasPrefix(s, "file:") {
		return ""
	}
	return s
}

func backupKey(passphrase string, salt []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, iter, 32)
}

func sealSecrets(plain []byte, passphrase string) (*backupSecrets, error) {
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	rand.Read(salt)
	rand.Read(nonce)
	key, err := backupKey(passphrase, salt, backupKDFIter)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &backupSecrets{KDF: "pbkdf2-sha256", Iter: backupKDFIter, Salt: salt, Nonce: nonce,
		Data: gcm.Seal(nil, nonce, plain, []byte("IRChatterbox backup"))}, nil
}

func openSecrets(s *backupSecrets, passphrase string) ([]byte, error) {
	if s.KDF != "pbkdf2-sha256" || s.Iter < 100_000 || s.Iter > 10_000_000 || len(s.Salt) < 16 || len(s.Nonce) != 12 {
		return nil, errors.New("The passwords in this backup can't be read.")
	}
	key, err := backupKey(passphrase, s.Salt, s.Iter)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, s.Nonce, s.Data, []byte("IRChatterbox backup"))
	if err != nil {
		return nil, errors.New(errWrongBackupPw)
	}
	return plain, nil
}

// writeBackupFile is shared by the desktop save dialog.
func writeBackupFile(path, data string) error {
	return os.WriteFile(path, []byte(data), 0o600)
}
