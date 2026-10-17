//go:build !windows && !android

package core

func setAutostart(on, minimized bool) error { return nil }
func autostartEnabled() bool                { return false }
