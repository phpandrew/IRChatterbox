//go:build !android

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// OpenDCCFolder opens the folder received files are saved to.
func (a *App) OpenDCCFolder() {
	dir := a.dccFolder()
	os.MkdirAll(dir, 0o755)
	openFolder(dir)
}

// ShowDCCFile shows a received or sent file in the file manager, selected where possible.
func (a *App) ShowDCCFile(id string) {
	dccMu.Lock()
	it := dccItems[id]
	path := ""
	if it != nil && it.State == "done" {
		path = it.Path
	}
	dccMu.Unlock()
	if path == "" {
		return
	}
	switch runtime.GOOS {
	case "windows":
		exec.Command("explorer", "/select,", path).Start()
	case "darwin":
		exec.Command("open", "-R", path).Start()
	default:
		openFolder(filepath.Dir(path))
	}
}

// PickDCCFolder chooses where received files go.
func (a *App) PickDCCFolder() string { return a.PickFolder("Save received files to") }
