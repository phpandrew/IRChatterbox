//go:build !windows && !android && !ios

package core

const windowClass = "IRChatterboxWindow"

func captureWindow() (WindowState, bool)               { return WindowState{}, false }
func restoreWindow(s WindowState, minimized bool) bool { return false }
func afterUnfullscreen()                               {}
