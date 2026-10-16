package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"unicode/utf16"
)

// NowPlaying is what the current song looks like, from whichever source found it.
type NowPlaying struct {
	Artist string `json:"artist"`
	Title  string `json:"title"`
	Album  string `json:"album"`
	Player string `json:"player"`
	Source string `json:"source"`
	Raw    string `json:"raw"`
}

// Text is "Artist - Title", or whatever the source gave us.
func (n NowPlaying) Text() string {
	switch {
	case n.Artist != "" && n.Title != "":
		return n.Artist + " - " + n.Title
	case n.Title != "":
		return n.Title
	default:
		return n.Raw
	}
}

func (n NowPlaying) Format(tpl string) string {
	if tpl == "" {
		tpl = "Now playing: {np}"
	}
	r := strings.NewReplacer("{np}", n.Text(), "{artist}", n.Artist, "{title}", n.Title, "{album}", n.Album, "{player}", n.Player)
	out := r.Replace(tpl)
	// tidy up when a field was empty, e.g. "Now playing:  - Title"
	out = strings.ReplaceAll(out, "()", "")
	return strings.Join(strings.Fields(out), " ")
}

var errNothingPlaying = errors.New("nothing is playing")

// getNowPlaying tries the configured source; "auto" tries Windows media controls,
// then the MusicBee window title, then the text file.
func getNowPlaying(m MusicSettings) (NowPlaying, error) {
	try := []string{m.Source}
	if m.Source == "" || m.Source == "auto" {
		try = []string{"smtc", "window", "file"}
	}
	var lastErr error = errNothingPlaying
	for _, src := range try {
		var np NowPlaying
		var err error
		switch src {
		case "smtc":
			np, err = nowPlayingSMTC()
		case "window":
			np, err = nowPlayingWindow()
		case "file":
			if m.File == "" {
				if m.Source == "file" {
					err = errors.New("no file set — choose the file your MusicBee plugin writes to")
				} else {
					continue
				}
			} else {
				np, err = nowPlayingFile(m.File)
			}
		}
		if err == nil && np.Text() != "" {
			np.Source = src
			return np, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	return NowPlaying{}, lastErr
}

// nowPlayingFile reads a text file such as the one written by MusicBee's
// "Now Playing to External Files" plugin. UTF-8 and UTF-16 are both handled.
func nowPlayingFile(path string) (NowPlaying, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return NowPlaying{}, err
	}
	var text string
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		le := b[0] == 0xFF
		b = b[2:]
		u := make([]uint16, len(b)/2)
		for i := range u {
			if le {
				u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
			} else {
				u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
			}
		}
		text = string(utf16.Decode(u))
	default:
		text = string(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}))
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return NowPlaying{}, errNothingPlaying
	}
	np := NowPlaying{Raw: strings.Join(lines, " - "), Player: "MusicBee"}
	if len(lines) >= 2 { // common layout: artist on line 1, title on line 2
		np.Artist, np.Title = lines[0], lines[1]
		if len(lines) >= 3 {
			np.Album = lines[2]
		}
	} else if a, t, ok := strings.Cut(lines[0], " - "); ok {
		np.Artist, np.Title = strings.TrimSpace(a), strings.TrimSpace(t)
	}
	return np, nil
}

// parseWindowTitle turns "Artist - Title - MusicBee" into fields.
func parseWindowTitle(title string) (NowPlaying, error) {
	t := strings.TrimSpace(title)
	for _, suf := range []string{" - MusicBee", " – MusicBee", " | MusicBee"} {
		t = strings.TrimSuffix(t, suf)
	}
	if t == "" || strings.EqualFold(t, "MusicBee") {
		return NowPlaying{}, errNothingPlaying
	}
	np := NowPlaying{Raw: t, Player: "MusicBee"}
	if a, rest, ok := strings.Cut(t, " - "); ok {
		np.Artist, np.Title = strings.TrimSpace(a), strings.TrimSpace(rest)
	} else {
		np.Title = t
	}
	return np, nil
}
