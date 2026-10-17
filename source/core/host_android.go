//go:build android

// Android host: the app runs inside the Android shell (android/), which hosts the same
// frontend in a WebView and drives this package through irchatterbox/mobile. The desktop
// host is desktop.go.

package core

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const updateAsset = "IRChatterbox.apk"

// Host is what the Android shell provides: events to the WebView, system notifications
// and opening links in the browser.
type Host interface {
	Emit(event string, data any)
	Notify(title, body string, data map[string]any, silent bool) error
	OpenURL(url string)
	// Connected reports how many servers are connected, so the shell can start or stop
	// its keep-alive foreground service.
	Connected(n int)
}

var (
	hostMu sync.Mutex
	host   Host

	bgMu       sync.Mutex
	background time.Time // when the app went to the background; zero while in front
)

// SetDataDir makes the app keep its config, logs, themes and plugins in dir (the app's
// private files folder). Must be called before NewApp.
func SetDataDir(dir string) { dataDirOverride = dir }

func currentHost() Host {
	hostMu.Lock()
	defer hostMu.Unlock()
	return host
}

// StartAndroid connects the app to the Android shell and starts its background loops.
func (a *App) StartAndroid(h Host) {
	hostMu.Lock()
	host = h
	hostMu.Unlock()
	a.notifyOK = true
	startOnce.Do(func() {
		a.plugins.LoadScripts()
		go a.autoAwayLoop()
		go a.autoUpdateLoop()
		go a.connectionWatch()
	})
}

var startOnce sync.Once

// connectionWatch tells the shell how many servers are connected whenever that changes.
func (a *App) connectionWatch() {
	last := -1
	for range time.Tick(2 * time.Second) {
		n := 0
		a.mu.Lock()
		for _, c := range a.conns {
			if s := c.Status(); s == "connected" || s == "connecting" {
				n++
			}
		}
		a.mu.Unlock()
		if n != last {
			last = n
			if h := currentHost(); h != nil {
				h.Connected(n)
			}
		}
	}
}

// SetBackground is called by the shell when the app leaves or returns to the screen.
// With Keep connection live off, going to the background disconnects every server and
// coming back reconnects the ones that were connected; the bouncer fills the gap.
func (a *App) SetBackground(bg bool) {
	bgMu.Lock()
	if bg {
		background = time.Now()
	} else {
		background = time.Time{}
	}
	bgMu.Unlock()
	a.SetFocus(!bg)
	if a.store.Get().Settings.KeepAlive {
		return
	}
	if bg {
		a.mu.Lock()
		a.parked = map[string]bool{}
		var conns []*Conn
		for id, c := range a.conns {
			if c.Status() != "disconnected" {
				a.parked[id] = true
				conns = append(conns, c)
			}
		}
		a.mu.Unlock()
		for _, c := range conns {
			c.Disconnect(a.store.Get().Settings.QuitMessage)
		}
		return
	}
	a.mu.Lock()
	ids := a.parked
	a.parked = nil
	a.mu.Unlock()
	for id := range ids {
		a.Connect(id)
	}
}

// Shutdown disconnects everything; called when the user quits from the app or the
// notification.
func (a *App) Shutdown() {
	msg := a.store.Get().Settings.QuitMessage
	a.mu.Lock()
	conns := make([]*Conn, 0, len(a.conns))
	for _, c := range a.conns {
		conns = append(conns, c)
	}
	a.mu.Unlock()
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *Conn) { defer wg.Done(); c.Disconnect(msg) }(c)
	}
	wg.Wait()
}

func (a *App) emitUI(name string, data any) {
	if h := currentHost(); h != nil {
		h.Emit(name, data)
	}
}

func (a *App) openBrowser(u string) {
	if h := currentHost(); h != nil {
		h.OpenURL(u)
	}
}

func (a *App) pushToast(title, body string, data map[string]any, silent bool) error {
	if h := currentHost(); h != nil {
		return h.Notify(title, body, data, silent)
	}
	return nil
}

// systemIdle is how long the app has been in the background, so auto-away marks you away
// after you've put the phone down for a while.
func systemIdle() time.Duration {
	bgMu.Lock()
	defer bgMu.Unlock()
	if background.IsZero() {
		return 0
	}
	return time.Since(background)
}

// Desktop-only features: no tray, taskbar, autostart or window state on Android.
func (a *App) startTray()                      {}
func setTrayAttention(on bool, tooltip string) {}
func flashTaskbar(on bool)                     {}
func setAutostart(on, minimized bool) error    { return nil }

// UI calls that only mean something on the desktop.
func (a *App) Quit(dontAsk bool)              { a.Shutdown() }
func (a *App) ToggleFullscreen()              {}
func (a *App) PickFolder(title string) string { return "" }
func (a *App) PickFile(title string) string   { return "" }
func (a *App) PickSoundFile() string          { return "" }
func (a *App) OpenDataFolder()                {}
func (a *App) OpenLogFolder()                 {}
func (a *App) ApplyLocalUpdate() string       { return "" }

// InstallUpdate opens the new APK's download; Android's installer takes it from there.
func (a *App) InstallUpdate() string {
	a.mu.Lock()
	info := a.lastUpdate
	a.mu.Unlock()
	if info.assetURL == "" {
		var err error
		if info, err = fetchLatestRelease(); err != nil {
			return err.Error()
		}
	}
	if info.assetURL == "" || !strings.HasPrefix(info.assetURL, "https://") {
		return fmt.Sprintf("No Android download in release %s", info.Latest)
	}
	a.openBrowser(info.assetURL)
	return ""
}
