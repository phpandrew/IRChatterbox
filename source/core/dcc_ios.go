//go:build ios

package core

// iOS: received files stay in the app's own folder; there's no file manager to open.
func (a *App) OpenDCCFolder()        {}
func (a *App) ShowDCCFile(id string) {}
func (a *App) PickDCCFolder() string { return "" }
