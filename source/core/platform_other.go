//go:build !windows && !android

package core

import (
	"syscall"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) startTray()                      {}
func stopTray()                                {}
func setTrayAttention(on bool, tooltip string) {}
func flashTaskbar(on bool)                     {}
func systemIdle() time.Duration                { return 0 }

// waitForPid waits (up to 15s) for the old process to exit after an update restart.
func waitForPid(pid uint32) {
	for i := 0; i < 150; i++ {
		if syscall.Kill(int(pid), 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func (a *App) pushToast(title, body string, data map[string]any, silent bool) error {
	if a.ctx == nil {
		return nil
	}
	return wruntime.SendNotification(a.ctx, wruntime.NotificationOptions{ID: newID(), Title: title, Body: body, Data: data})
}
