//go:build windows

package core

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowClass = "IRChatterboxWindow"

var (
	procFindWindowW        = user32.NewProc("FindWindowW")
	procGetWindowPlacement = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement = user32.NewProc("SetWindowPlacement")
)

type winPoint struct{ X, Y int32 }
type winRect struct{ Left, Top, Right, Bottom int32 }
type windowPlacement struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	MinPosition    winPoint
	MaxPosition    winPoint
	NormalPosition winRect
}

const (
	swShowNormal    = 1
	swShowMinimized = 2
	swShowMaximized = 3
)

func mainHWND() uintptr {
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(windowClass))), 0)
	return h
}

// captureWindow reads the window's normal (restored) rectangle and whether it's maximised.
// Using the "normal" rect means a maximised window still remembers its restore size.
func captureWindow() (WindowState, bool) {
	h := mainHWND()
	if h == 0 {
		return WindowState{}, false
	}
	var wp windowPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	if r, _, _ := procGetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&wp))); r == 0 {
		return WindowState{}, false
	}
	r := wp.NormalPosition
	return WindowState{
		X: int(r.Left), Y: int(r.Top), Width: int(r.Right - r.Left), Height: int(r.Bottom - r.Top),
		Maximized: wp.ShowCmd == swShowMaximized, Saved: true,
	}, true
}

// restoreWindow puts the window back where it was. Windows moves it onto a visible
// monitor by itself if that monitor is no longer connected.
func restoreWindow(s WindowState, minimized bool) bool {
	h := mainHWND()
	if h == 0 || !s.Saved || s.Width < 200 || s.Height < 150 {
		return false
	}
	wp := windowPlacement{
		ShowCmd:        swShowNormal,
		NormalPosition: winRect{int32(s.X), int32(s.Y), int32(s.X + s.Width), int32(s.Y + s.Height)},
	}
	wp.Length = uint32(unsafe.Sizeof(wp))
	if s.Maximized {
		wp.ShowCmd = swShowMaximized
	}
	if minimized {
		wp.ShowCmd = swShowMinimized
		if s.Maximized {
			wp.Flags = 0x2 // WPF_RESTORETOMAXIMIZED
		}
	}
	r, _, _ := procSetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&wp)))
	return r != 0
}
