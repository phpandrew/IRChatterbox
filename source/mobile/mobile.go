//go:build android

// Package mobile is the Android entry point, built into an .aar with gomobile bind
// (see android/build-core.sh).
//
// The Android shell hosts the same frontend in a WebView. Calls the UI makes on
// window.go.core.App arrive here as Call(method, argsJSON), the same way Wails dispatches
// them on Windows, and events go back out through Host.Emit.
package mobile

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"irchatterbox/core"
)

// Host is implemented in Kotlin (CoreHost.kt).
type Host interface {
	// Emit delivers a backend event (window.runtime.EventsOn) as JSON.
	Emit(event, dataJSON string)
	// ShowNotification shows a system notification; tapping it opens server/buffer.
	ShowNotification(title, body, server, buffer string, silent bool)
	// OpenURL opens a link outside the app.
	OpenURL(url string)
	// Connected reports how many servers are connected (keep-alive service on/off).
	Connected(n int)
}

var (
	once sync.Once
	mu   sync.Mutex
	app  *core.App
	h    Host
)

// Start creates the app the first time it's called; later calls only swap the host
// (the Activity can be recreated while the process and connections live on).
func Start(dataDir, version string, host Host) {
	mu.Lock()
	h = host
	mu.Unlock()
	once.Do(func() {
		core.SetDataDir(dataDir)
		core.SetVersion(version, "")
		app = core.NewApp()
	})
	app.StartAndroid(adapter{})
}

// SetStore marks the build as installed from an app store ("play"): the store handles
// updates. Call it before Start.
func SetStore(name string) { core.SetStore(name) }

// SetBackground tells the app it has left (true) or returned to (false) the screen.
func SetBackground(bg bool) {
	if app != nil {
		app.SetBackground(bg)
	}
}

// KeepAlive reports whether "Keep connection live in background" is on.
func KeepAlive() bool {
	return app != nil && app.GetConfig().Settings.KeepAlive
}

// Shutdown disconnects every server (Quit from the app or its notification).
func Shutdown() {
	if app != nil {
		app.Shutdown()
	}
}

// Methods the UI must not reach: they're for the shell itself.
var internal = map[string]bool{"StartAndroid": true, "SetStore": true, "SetBackground": true, "Shutdown": true}

// Call runs an App method by name with a JSON array of arguments and returns
// {"ok": result} or {"error": message}.
func Call(method, argsJSON string) string {
	if app == nil {
		return encodeReply(nil, errors.New("not started"))
	}
	if internal[method] {
		return encodeReply(nil, fmt.Errorf("%s isn't available to the UI", method))
	}
	return encodeReply(dispatch(app, method, argsJSON))
}

func host() Host {
	mu.Lock()
	defer mu.Unlock()
	return h
}

// adapter turns core's Go-typed host calls into the string-based gomobile Host.
type adapter struct{}

func (adapter) Emit(event string, data any) {
	hh := host()
	if hh == nil {
		return
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	hh.Emit(event, string(b))
}

func (adapter) Notify(title, body string, data map[string]any, silent bool) error {
	if hh := host(); hh != nil {
		server, _ := data["server"].(string)
		buffer, _ := data["buffer"].(string)
		hh.ShowNotification(title, body, server, buffer, silent)
	}
	return nil
}

func (adapter) OpenURL(u string) {
	if hh := host(); hh != nil {
		hh.OpenURL(u)
	}
}

func (adapter) Connected(n int) {
	if hh := host(); hh != nil {
		hh.Connected(n)
	}
}
