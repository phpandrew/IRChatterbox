//go:build !windows

package main

func setAutostart(on, minimized bool) error { return nil }
func autostartEnabled() bool              { return false }
