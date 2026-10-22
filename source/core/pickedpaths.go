package core

// Paths the user chose this session, through a native file dialog or by typing them in a
// command. Methods that read or send a file given as a path only accept these, so a bug in
// the chat view (or a script, or an imported backup) can't turn them into "read any file".

import (
	"path/filepath"
	"strings"
	"sync"
)

var (
	pickedMu    sync.Mutex
	pickedPaths = map[string]bool{}
)

func normPath(p string) string {
	p = filepath.Clean(strings.TrimSpace(p))
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}

// allowPath records a path the user picked or typed.
func allowPath(p string) string {
	if p == "" {
		return ""
	}
	pickedMu.Lock()
	pickedPaths[normPath(p)] = true
	pickedMu.Unlock()
	return p
}

// pathAllowed reports whether the user chose this path this session.
func pathAllowed(p string) bool {
	if p == "" {
		return false
	}
	pickedMu.Lock()
	defer pickedMu.Unlock()
	return pickedPaths[normPath(p)]
}

// soundPathOK: a custom notification sound must be one the user picked, or the one already
// saved in Settings, and must be an audio file.
func (a *App) soundPathOK(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".wav", ".mp3", ".ogg", ".m4a", ".flac":
	default:
		return false
	}
	set := a.store.Get().Settings // stored as "file:<path>"
	return pathAllowed(p) || (p != "" && ("file:"+p == set.SoundHighlight || "file:"+p == set.SoundPM))
}

// validColor: a CSS hex colour (#rgb, #rrggbb or #rrggbbaa). Colours end up inside style
// attributes, so nothing else is accepted.
func validColor(c string) bool {
	c = strings.TrimSpace(c)
	if len(c) != 4 && len(c) != 7 && len(c) != 9 || c[0] != '#' {
		return false
	}
	for _, r := range c[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// validID: a server ID as newID makes them (hex), or the plain ASCII a hand-edited or
// imported config might use.
func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
