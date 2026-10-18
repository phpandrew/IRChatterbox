//go:build android

package core

func (a *App) OpenDCCFolder()        {}
func (a *App) ShowDCCFile(id string) {}
func (a *App) PickDCCFolder() string { return "" }
