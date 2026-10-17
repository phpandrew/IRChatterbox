package main

import (
	"embed"
	"os"
	"strconv"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// a new build dropped next to the exe as IRChatterbox.exe.new? swap it in and restart
	applyPendingBuildAtLaunch()
	// after an auto-update the new exe is started with --wait-pid <old pid>: wait for the old
	// one to exit so the single-instance lock is free
	for i, a := range os.Args[1:] {
		if a == "--wait-pid" && i+2 < len(os.Args) {
			if pid, err := strconv.Atoi(os.Args[i+2]); err == nil {
				waitForPid(pid)
			}
		}
	}
	app := NewApp()
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
		Title:     "IRChatterbox",
		Width:     w,
		Height:    h,
		StartHidden: app.store.Get().Window.Saved, // shown after being moved into place, so it doesn't jump
		MinWidth:  720,
		MinHeight: 480,
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
		Windows: &windows.Options{
			Theme:                windows.SystemDefault, // title bar follows Windows light/dark
			WebviewIsTransparent: false,
			WindowClassName:      windowClass,
		},
		// WebView2's own right-click menu: spelling suggestions in the message box and Copy on
		// selected text. Wails only shows it on inputs/selections (see --default-contextmenu);
		// the app's own menus (nicks, channels, links) still take priority.
		EnableDefaultContextMenu: true,
		Bind: []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
