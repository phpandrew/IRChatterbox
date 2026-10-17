//go:build !windows && !android

package core

import (
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) startTray()                      {}
func stopTray()                                {}
func setTrayAttention(on bool, tooltip string) {}
func flashTaskbar(on bool)                     {}
func systemIdle() time.Duration                { return 0 }
func waitForPid(pid uint32)                    {}
func (a *App) pushToast(title, body string, data map[string]any, silent bool) error {
	if a.ctx == nil {
		return nil
	}
	return wruntime.SendNotification(a.ctx, wruntime.NotificationOptions{ID: newID(), Title: title, Body: body, Data: data})
}
