//go:build android || ios

// Phone host, shared by Android and iOS: the app runs inside a native shell (android/ or
// ios/) that hosts the same frontend in a WebView and drives this package through
// irchatterbox/mobile. The desktop host is desktop.go; what differs between the two phone
// platforms (updates) is in host_android.go and host_ios.go.

package core

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Host is what the phone shell provides: events to the WebView, system notifications
// and opening links in the browser.
type Host interface {
	Emit(event string, data any)
	Notify(title, body string, data map[string]any, silent bool) error
	OpenURL(url string)
	// Connected reports how many servers are connected, so the shell can start or stop
	// its keep-alive foreground service.
	Connected(n int)
	// InstallAPK hands a downloaded update to Android's package installer, which asks the
	// user to confirm. Returns an error message, or "" once the installer has it. iOS never
	// calls it: TestFlight and the App Store deliver updates.
	InstallAPK(path string) string
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

// StartAndroid connects the app to the phone shell (the name predates iOS) and starts its background loops.
func (a *App) StartAndroid(h Host) {
	hostMu.Lock()
	host = h
	hostMu.Unlock()
	a.notifyOK = true
	startOnce.Do(func() {
		os.RemoveAll(updatesDir()) // the APK from an update that's now installed (or abandoned)
		a.plugins.LoadScripts()
		go a.autoAwayLoop()
		go a.autoUpdateLoop()
		a.startSync()
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
			// running includes the wait between reconnect attempts, when Status() says
			// "disconnected"; the keep-alive service must stay up through that
			if c.isRunning() {
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
// coming back reconnects the ones that were connected; the bouncer fills the gap. iOS has no
// keep-alive (it suspends apps in the background), so there it always works that way; the
// iOS shell calls SetBackground(true) about 20 seconds after you leave, so a quick switch to
// another app doesn't drop the connection.
func (a *App) SetBackground(bg bool) {
	bgMu.Lock()
	if bg {
		background = time.Now()
	} else {
		background = time.Time{}
	}
	bgMu.Unlock()
	a.SetFocus(!bg)
	if keepAliveSupported && a.store.Get().Settings.KeepAlive {
		return
	}
	if bg {
		a.mu.Lock()
		a.parked = map[string]bool{}
		var conns []*Conn
		for id, c := range a.conns {
			if c.isRunning() {
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

// Desktop-only features: no tray, taskbar, autostart or window state on a phone.
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
func (a *App) OpenDiagnostics()               {} // phones: "Share app log" in the shell
func (a *App) OpenLogFolder()                 {}
func (a *App) ApplyLocalUpdate() string       { return "" }

// The phone shell saves and opens backup files itself (MainActivity / BridgeHandler.swift,
// via bridge.js).
func (a *App) SaveBackup(name, data string) string { return "Not available on this device" }
func (a *App) OpenBackup() BackupOpen              { return BackupOpen{Error: "Not available on this device"} }

// updatesDir holds a downloaded update until Android has installed it (unused on iOS).
func updatesDir() string { return filepath.Join(appDir(), "updates") }
