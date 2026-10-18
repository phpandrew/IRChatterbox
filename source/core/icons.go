package core

// Channel icons: a small picture you pick for a channel or private chat, shown in the sidebar
// and the header (like server icons in Discord). They're yours only, kept in icons.json in the
// data folder; IRC has no way to share them. The UI shrinks the picture to 128×128 first.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxIconBytes = 256 << 10

var iconsMu sync.Mutex

func (a *App) iconsPath() string { return filepath.Join(a.store.dir, "icons.json") }

func iconKey(server, buffer string) string { return server + "/" + lower(buffer) }

func (a *App) readIcons() map[string]string {
	icons := map[string]string{}
	if b, err := os.ReadFile(a.iconsPath()); err == nil {
		json.Unmarshal(b, &icons)
	}
	return icons
}

// GetChannelIcons returns every icon as "serverID/channel" → data URL.
func (a *App) GetChannelIcons() map[string]string {
	iconsMu.Lock()
	defer iconsMu.Unlock()
	return a.readIcons()
}

// SetChannelIcon saves (or with an empty dataURL removes) a channel's icon. Returns an error to show.
func (a *App) SetChannelIcon(server, buffer, dataURL string) string {
	if server == "" || buffer == "" {
		return "No channel"
	}
	if dataURL != "" {
		ok := false
		for _, p := range []string{"data:image/png;base64,", "data:image/webp;base64,", "data:image/jpeg;base64,"} {
			if strings.HasPrefix(dataURL, p) {
				raw, err := base64.StdEncoding.DecodeString(dataURL[len(p):])
				ok = err == nil && len(raw) > 0 && len(raw) <= maxIconBytes
			}
		}
		if !ok {
			return "That picture can't be used as an icon"
		}
	}
	iconsMu.Lock()
	defer iconsMu.Unlock()
	icons := a.readIcons()
	if dataURL == "" {
		delete(icons, iconKey(server, buffer))
	} else {
		icons[iconKey(server, buffer)] = dataURL
	}
	b, _ := json.Marshal(icons)
	tmp := a.iconsPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return "The icon couldn't be saved"
	}
	if err := os.Rename(tmp, a.iconsPath()); err != nil {
		return "The icon couldn't be saved"
	}
	a.emitUI("icons", icons)
	return ""
}
