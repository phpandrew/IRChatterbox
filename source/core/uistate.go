package core

// Window layout the UI remembers between launches (column widths, collapsed servers, the
// compact sidebar, split view…), kept in ui.json in the data folder. The web view's own
// storage is tied to the exe's file name, so it's lost when you run a renamed or newly
// downloaded copy; this file isn't.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const uiStateMax = 256 * 1024

// GetUIState returns the saved layout as JSON, or "" when there's none.
func (a *App) GetUIState() string {
	b, err := os.ReadFile(filepath.Join(a.store.dir, "ui.json"))
	if err != nil || !json.Valid(b) {
		return ""
	}
	return string(b)
}

// SaveUIState stores the layout (a JSON object from the UI).
func (a *App) SaveUIState(state string) {
	if len(state) > uiStateMax || !json.Valid([]byte(state)) || len(state) == 0 || state[0] != '{' {
		return
	}
	p := filepath.Join(a.store.dir, "ui.json")
	tmp := p + ".tmp"
	if os.WriteFile(tmp, []byte(state), 0o600) == nil {
		os.Rename(tmp, p)
	}
}
