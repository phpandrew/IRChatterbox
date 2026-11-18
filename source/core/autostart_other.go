//go:build !windows && !android && !ios

package core

func setAutostart(on, minimized bool) error { return nil }
func autostartEnabled() bool                { return false }
