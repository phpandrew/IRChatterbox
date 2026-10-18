//go:build !android

// Desktop host: the Wails window, its lifecycle, native dialogs and the bridge to the UI.
// The Android host lives in host_android.go.

package core

import (
	"context"
	_ "embed"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed assets/icon-128.png
var desktopIcon []byte

// RunDesktop starts the Windows app. assets is the built frontend (frontend/dist).
func RunDesktop(assets fs.FS) {
	// a new build dropped next to the exe as IRChatterbox.exe.new? swap it in and restart
	applyPendingBuildAtLaunch()
	// after an auto-update the new exe is started with --wait-pid <old pid>: wait for the old
	// one to exit so the single-instance lock is free
	for i, a := range os.Args[1:] {
		if a == "--wait-pid" && i+2 < len(os.Args) {
			// PIDs are 32-bit on Windows; ParseUint rejects anything that doesn't fit
			if pid, err := strconv.ParseUint(os.Args[i+2], 10, 32); err == nil {
				waitForPid(uint32(pid))
			}
		}
	}
	app := NewApp()
	app.initLogs()
	startState := options.Normal
	for _, a := range os.Args[1:] {
		if a == "--minimized" || a == "/minimized" {
			startState = options.Minimised
			app.startMinimized = true
		}
	}
	// open at the last saved size; the exact position/monitor is restored once the window exists
	w, h := 1400, 880
	if ws := app.store.Get().Window; ws.Saved && ws.Width >= 720 && ws.Height >= 480 {
		w, h = ws.Width, ws.Height
	}

	err := wails.Run(&options.App{
		Title:            "IRChatterbox",
		Width:            w,
		Height:           h,
		StartHidden:      app.store.Get().Window.Saved, // shown after being moved into place, so it doesn't jump
		MinWidth:         720,
		MinHeight:        480,
		WindowStartState: startState,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 22, G: 25, B: 31, A: 255},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "irchatterbox-6f2c1e0a-3d4b-4c55-9f61-chatterbox",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.secondInstance() },
		},
		Linux: &linux.Options{
			ProgramName: "IRChatterbox", // window class, so the dock/taskbar groups it properly
			Icon:        desktopIcon,
		},
		Mac: &mac.Options{
			About: &mac.AboutInfo{Title: "IRChatterbox", Message: "A modern IRC client.\nhttps://github.com/phpandrew/IRChatterbox", Icon: desktopIcon},
		},
		Windows: &windows.Options{
			Theme:                windows.SystemDefault, // title bar follows Windows light/dark
			WebviewIsTransparent: false,
			WindowClassName:      windowClass,
		},
		// WebView2's own right-click menu: spelling suggestions in the message box and Copy on
		// selected text. Wails only shows it on inputs/selections (see --default-contextmenu);
		// the app's own menus (nicks, channels, links) still take priority.
		EnableDefaultContextMenu: true,
		Bind:                     []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := wruntime.InitializeNotifications(ctx); err == nil {
		a.notifyOK = true
		wruntime.OnNotificationResponse(ctx, func(r wruntime.NotificationResult) {
			if r.Error != nil {
				return
			}
			info := r.Response.UserInfo
			wruntime.WindowUnminimise(ctx)
			wruntime.WindowShow(ctx)
			wruntime.EventsEmit(ctx, "focusBuffer", map[string]any{"server": info["server"], "buffer": info["buffer"]})
		})
	}
	cleanupOldExe()
	a.plugins.LoadScripts()
	if hasTray && a.store.Get().Settings.TrayIcon {
		a.trayStarted = true
		a.startTray()
	}
	if set := a.store.Get().Settings; set.StartWithWindows {
		setAutostart(true, set.StartMinimized)
	}
}

func (a *App) domReady(ctx context.Context) {
	a.mu.Lock()
	if a.windowShown {
		a.mu.Unlock()
		return
	}
	a.windowShown = true
	a.mu.Unlock()
	ws := a.store.Get().Window
	if ws.Saved {
		restoreWindow(ws, a.startMinimized)
		if !a.startMinimized {
			wruntime.WindowShow(ctx)
			if ws.Fullscreen {
				wruntime.WindowFullscreen(ctx)
			}
		}
	}
	go a.autoUpdateLoop()
	go a.watchPendingBuild()
	go a.autoAwayLoop()
	// keep the saved window state fresh in case of a crash or power loss
	go func() {
		for range time.Tick(30 * time.Second) {
			a.saveWindow()
		}
	}()
}

// saveWindow stores the window's size, position, maximised and full-screen state.
func (a *App) saveWindow() {
	if a.ctx == nil {
		return
	}
	ws, ok := captureWindow()
	if !ok {
		return
	}
	if wruntime.WindowIsMinimised(a.ctx) {
		// keep the last known good state, but don't record "minimised"
		ws.Maximized = a.store.Get().Window.Maximized
	}
	ws.Fullscreen = wruntime.WindowIsFullscreen(a.ctx)
	if ws.Fullscreen {
		// in full screen the "normal" rect is the screen; keep the earlier restore size
		prev := a.store.Get().Window
		ws.X, ws.Y, ws.Width, ws.Height, ws.Maximized = prev.X, prev.Y, prev.Width, prev.Height, prev.Maximized
		ws.Saved = prev.Saved || ws.Saved
	}
	if a.store.Get().Window == ws {
		return
	}
	a.store.Update(func(c *Config) { c.Window = ws })
}

// ToggleFullscreen switches full-screen mode (F11).
func (a *App) ToggleFullscreen() {
	if wruntime.WindowIsFullscreen(a.ctx) {
		wruntime.WindowUnfullscreen(a.ctx)
	} else {
		a.saveWindow()
		wruntime.WindowFullscreen(a.ctx)
	}
	go func() { time.Sleep(300 * time.Millisecond); a.saveWindow() }()
}

func (a *App) shutdown(ctx context.Context) {
	stopTray()
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

// beforeClose runs when the window's close button is pressed. Returning true keeps the
// window open while the UI asks for confirmation.
func (a *App) beforeClose(ctx context.Context) bool {
	// "close to tray": hide instead of quitting (Quit from the tray menu exits)
	if set := a.store.Get().Settings; set.CloseToTray && a.trayStarted {
		a.mu.Lock()
		allow := a.allowQuit
		a.mu.Unlock()
		if !allow {
			a.saveWindow()
			wruntime.WindowHide(ctx)
			return true
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.allowQuit || !a.store.Get().Settings.ConfirmOnClose {
		a.saveWindow()
		return false
	}
	// pressing close again while the question is showing closes for real, so a stuck UI
	// can never trap you
	if time.Since(a.closeAsked) < 4*time.Second {
		a.saveWindow()
		return false
	}
	a.closeAsked = time.Now()
	var connected, bounced []string
	for _, s := range a.store.Get().Servers {
		if c, ok := a.conns[s.ID]; ok && c.Status() != "disconnected" {
			if s.Bouncer != "" {
				bounced = append(bounced, s.Name) // the bouncer keeps these online
			} else {
				connected = append(connected, s.Name)
			}
		}
	}
	wruntime.WindowUnminimise(ctx)
	wruntime.WindowShow(ctx)
	wruntime.EventsEmit(ctx, "confirmClose", map[string]any{"connected": connected, "bouncer": bounced})
	return true
}

// Quit closes the app after the user confirmed. dontAsk turns the confirmation off.
func (a *App) Quit(dontAsk bool) {
	if dontAsk {
		a.store.Update(func(c *Config) { c.Settings.ConfirmOnClose = false })
	}
	a.saveWindow()
	a.mu.Lock()
	a.allowQuit = true
	a.mu.Unlock()
	wruntime.Quit(a.ctx)
}

func (a *App) secondInstance() {
	if a.ctx != nil {
		wruntime.WindowUnminimise(a.ctx)
		wruntime.WindowShow(a.ctx)
	}
}

func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	wruntime.WindowShow(a.ctx)
	wruntime.WindowUnminimise(a.ctx)
	a.clearAttention()
}

// quitNow exits without asking (tray menu → Quit).
func (a *App) quitNow() {
	a.saveWindow()
	a.mu.Lock()
	a.allowQuit = true
	a.mu.Unlock()
	wruntime.Quit(a.ctx)
}

// PickFolder opens a folder chooser and returns the chosen path.
func (a *App) PickFolder(title string) string {
	p, _ := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
	return p
}

// PickFile opens a file chooser and returns the chosen path.
func (a *App) PickFile(title string) string {
	p, _ := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: title,
		Filters: []wruntime.FileFilter{{DisplayName: "Text files (*.txt)", Pattern: "*.txt"}, {DisplayName: "All files", Pattern: "*.*"}}})
	return p
}

// SaveBackup asks where to save a settings backup and writes it. "" = saved or cancelled.
func (a *App) SaveBackup(name, data string) string {
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Save settings backup", DefaultFilename: name,
		Filters: []wruntime.FileFilter{{DisplayName: "IRChatterbox backup (*.json)", Pattern: "*.json"}}})
	if err != nil {
		return err.Error()
	}
	if p == "" {
		return ""
	}
	if err := writeBackupFile(p, data); err != nil {
		return err.Error()
	}
	return ""
}

// OpenBackup asks for a settings backup and returns its text ("" when cancelled).
func (a *App) OpenBackup() BackupOpen {
	p, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Import settings backup",
		Filters: []wruntime.FileFilter{{DisplayName: "IRChatterbox backup (*.json)", Pattern: "*.json"}, {DisplayName: "All files", Pattern: "*.*"}}})
	if err != nil {
		return BackupOpen{Error: err.Error()}
	}
	if p == "" {
		return BackupOpen{}
	}
	st, err := os.Stat(p)
	if err != nil {
		return BackupOpen{Error: err.Error()}
	}
	if st.Size() > backupMaxSize {
		return BackupOpen{Error: "That file is too big to be an IRChatterbox backup."}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return BackupOpen{Error: err.Error()}
	}
	return BackupOpen{Data: string(b)}
}

// PickSoundFile opens a file chooser for a custom notification sound.
func (a *App) PickSoundFile() string {
	p, _ := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Choose a notification sound",
		Filters: []wruntime.FileFilter{{DisplayName: "Sounds (*.wav;*.mp3;*.ogg;*.m4a)", Pattern: "*.wav;*.mp3;*.ogg;*.m4a"}}})
	return p
}

func (a *App) OpenDataFolder() { openFolder(a.store.dir) }

// openFolder shows a folder in Explorer, Finder or the Linux file manager.
func openFolder(dir string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("explorer", dir).Start()
	case "darwin":
		exec.Command("open", dir).Start()
	default:
		exec.Command("xdg-open", dir).Start()
	}
}

// OpenLogFolder opens the folder the .txt logs are written to.
func (a *App) OpenLogFolder() {
	dir := a.textLogDir()
	os.MkdirAll(dir, 0o755)
	openFolder(dir)
}

// emitUI sends an event to the frontend (window.runtime.EventsOn).
func (a *App) emitUI(name string, data any) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, name, data)
	}
}

func (a *App) openBrowser(u string) {
	if a.ctx != nil {
		wruntime.BrowserOpenURL(a.ctx, u)
	}
}
