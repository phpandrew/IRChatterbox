package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
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
		Bind: []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
