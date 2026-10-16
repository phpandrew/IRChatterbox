package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Two kinds of log are kept per channel/query:
//   logs/<network>/<#channel>.jsonl  — structured history used to reload scrollback
//   <log folder>/<network>/<#channel>.txt — optional readable log ("[12:34:56] <nick> text")
// Both are trimmed to Settings.LogMaxLines.

func (a *App) logPath(server, buffer string) string {
	return filepath.Join(a.store.dir, "logs", safeName(server), safeName(lower(buffer))+".jsonl")
}

func (a *App) textLogDir() string {
	if d := strings.TrimSpace(a.store.Get().Settings.LogFolder); d != "" {
		return d
	}
	return filepath.Join(a.store.dir, "logs")
}

func (a *App) textLogPath(server, buffer string) string {
	return filepath.Join(a.textLogDir(), safeName(server), safeName(lower(buffer))+".txt")
}

func (a *App) logEvent(server string, ev Event) {
	set := a.store.Get().Settings
	if !set.Logging && !set.TextLogs {
		return
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.logLines == nil {
		a.logLines = map[string]int{}
		a.logOpened = map[string]bool{}
	}
	// structured history: skip join/part/quit/mode noise
	if set.Logging && !(ev.Type == "event" && ev.Kind != "topic" && ev.Kind != "kick" && ev.Kind != "nick") {
		b, _ := json.Marshal(ev)
		a.appendLog(a.logPath(server, ev.Buffer), []string{string(b)}, set.LogMaxLines)
	}
	if set.TextLogs {
		p := a.textLogPath(server, ev.Buffer)
		var out []string
		if !a.logOpened[p] {
			a.logOpened[p] = true
			out = append(out, "--- Session started "+time.Now().Format("Mon Jan 2 2006 15:04:05"))
		}
		if l := textLine(ev); l != "" {
			out = append(out, l)
			a.appendLog(p, out, set.LogMaxLines)
		}
	}
}

// textLine renders an event the way classic IRC clients log it.
func textLine(ev Event) string {
	t := time.UnixMilli(ev.Time).Format("[2006-01-02 15:04:05]")
	text := stripFormatting(ev.Text)
	nick := ev.Prefix + ev.Nick
	switch ev.Type {
	case "msg":
		switch ev.Kind {
		case "action":
			return fmt.Sprintf("%s * %s %s", t, ev.Nick, text)
		case "notice":
			return fmt.Sprintf("%s -%s- %s", t, ev.Nick, text)
		default:
			return fmt.Sprintf("%s <%s> %s", t, nick, text)
		}
	case "event":
		reason := ""
		if text != "" {
			reason = " (" + text + ")"
		}
		switch ev.Kind {
		case "join":
			return fmt.Sprintf("%s *** %s (%s) has joined %s", t, ev.Nick, text, ev.Buffer)
		case "part":
			return fmt.Sprintf("%s *** %s has left %s%s", t, ev.Nick, ev.Buffer, reason)
		case "quit":
			return fmt.Sprintf("%s *** %s has quit%s", t, ev.Nick, reason)
		case "kick":
			victim, _ := ev.Extra["victim"].(string)
			return fmt.Sprintf("%s *** %s was kicked by %s%s", t, victim, ev.Nick, reason)
		case "nick":
			return fmt.Sprintf("%s *** %s is now known as %s", t, ev.Nick, text)
		case "mode":
			return fmt.Sprintf("%s *** %s sets mode %s", t, ev.Nick, text)
		case "topic":
			return fmt.Sprintf("%s *** %s changed the topic to: %s", t, ev.Nick, text)
		}
	}
	return ""
}

// appendLog adds lines to a file and trims it to the newest max lines (0 = unlimited).
// Trimming happens in batches (10% slack) so the file isn't rewritten on every line.
func (a *App) appendLog(path string, lines []string, max int) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	f.WriteString(strings.Join(lines, "\n") + "\n")
	f.Close()
	if max <= 0 {
		return
	}
	n, ok := a.logLines[path]
	if !ok {
		n = countLines(path)
	} else {
		n += len(lines)
	}
	if n > max+max/10+1 {
		n = trimFile(path, max)
	}
	a.logLines[path] = n
}

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	buf := make([]byte, 64*1024)
	for {
		c, err := f.Read(buf)
		n += bytes.Count(buf[:c], []byte{'\n'})
		if err != nil {
			return n
		}
	}
}

// trimFile keeps the last max lines of a file and returns the new line count.
func trimFile(path string, max int) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	ring := make([]string, 0, max)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if len(ring) == max {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	f.Close()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(ring, "\n")+"\n"), 0o644); err != nil {
		return len(ring)
	}
	os.Rename(tmp, path)
	return len(ring)
}

// TrimAllLogs applies the current line limit to every existing log right away
// (called when the limit is changed in Settings).
func (a *App) TrimAllLogs() {
	max := a.store.Get().Settings.LogMaxLines
	if max <= 0 {
		return
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.logLines == nil {
		a.logLines = map[string]int{}
		a.logOpened = map[string]bool{}
	}
	for _, root := range []string{filepath.Join(a.store.dir, "logs"), a.textLogDir()} {
		filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !(strings.HasSuffix(p, ".jsonl") || strings.HasSuffix(p, ".txt")) {
				return nil
			}
			if n := countLines(p); n > max {
				a.logLines[p] = trimFile(p, max)
			}
			return nil
		})
	}
}

var _ = io.EOF
