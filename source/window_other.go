//go:build !windows

package main

const windowClass = "IRChatterboxWindow"

func captureWindow() (WindowState, bool)            { return WindowState{}, false }
func restoreWindow(s WindowState, minimized bool) bool { return false }
