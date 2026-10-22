package core

// Multi-line messages (IRCv3 draft/multiline): a pasted block goes out as one message in a
// BATCH instead of a burst of separate lines, and arrives the same way for other clients
// that support it (the rest see ordinary lines). Incoming multi-line batches are shown as
// one message with line breaks.

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type multilineBatch struct {
	tags   map[string]string
	target string
	prefix string
	cmd    string
	text   strings.Builder
	lines  int
}

// multilineLimits returns the server's limits, ok=false when it doesn't support multiline.
func (c *Conn) multilineLimits() (maxBytes, maxLines int, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.caps["draft/multiline"] || !c.caps["batch"] {
		return 0, 0, false
	}
	maxBytes, maxLines = 4096, 0
	for _, cp := range c.capLS {
		name, val, _ := strings.Cut(cp, "=")
		if name != "draft/multiline" {
			continue
		}
		for _, kv := range strings.Split(val, ",") {
			k, v, _ := strings.Cut(kv, "=")
			n, err := strconv.Atoi(v)
			if err != nil {
				continue
			}
			switch k {
			case "max-bytes":
				maxBytes = n
			case "max-lines":
				maxLines = n
			}
		}
	}
	return maxBytes, maxLines, maxBytes > 0
}

// CanSendMultiline tells the UI whether a pasted block of lines would go out as one message.
func (a *App) CanSendMultiline(server string, lines int) bool {
	c := a.conn(server)
	if c == nil {
		return false
	}
	_, maxLines, ok := c.multilineLimits()
	return ok && (maxLines == 0 || lines <= maxLines)
}

// SayMultiline sends lines to target as one multi-line message. It returns false, sending
// nothing, when the server doesn't support that or the text is over its limits; the caller
// then sends the lines one by one.
func (c *Conn) SayMultiline(target string, lines []string) bool {
	if len(lines) < 2 || strings.HasPrefix(target, "=") || c.Status() != "connected" {
		return false
	}
	maxBytes, maxLines, ok := c.multilineLimits()
	if !ok || (maxLines > 0 && len(lines) > maxLines) {
		return false
	}
	text := strings.Join(lines, "\n")
	if len(text) > maxBytes {
		return false
	}
	type part struct {
		s      string
		concat bool
	}
	var parts []part
	payload := c.maxPayload("PRIVMSG", target) - 60 // room for the batch tag
	for _, l := range lines {
		chunks := splitExact(l, payload)
		if len(chunks) == 0 {
			chunks = []string{""}
		}
		for i, ch := range chunks {
			parts = append(parts, part{ch, i > 0})
		}
	}
	if maxLines > 0 && len(parts) > maxLines {
		return false
	}
	b := make([]byte, 6)
	rand.Read(b)
	ref := "ml" + hex.EncodeToString(b)

	c.mu.Lock()
	echo := c.caps["echo-message"]
	if echo {
		c.pending = append(c.pending, pendingMsg{target: target, text: text, at: time.Now()})
	}
	c.mu.Unlock()

	c.SendRaw("BATCH +" + ref + " draft/multiline " + target)
	for _, p := range parts {
		tags := "@batch=" + ref
		if p.concat {
			tags += ";draft/multiline-concat"
		}
		c.SendRaw(tags + " PRIVMSG " + target + " :" + p.s)
	}
	c.SendRaw("BATCH -" + ref)
	if !echo {
		c.emitOwn(target, "privmsg", text)
		c.app.plugins.OnMessage(c, target, c.Nick(), text, true)
	}
	return true
}

// Batches a server may leave open at once; more are ignored.
const (
	maxOpenBatches   = 256
	maxOpenMultiline = 16
)

// onMultiline collects incoming multi-line batches. It reports whether it took m; when a
// batch ends, the whole message is handled as one PRIVMSG/NOTICE.
func (c *Conn) onMultiline(m *Message) bool {
	if m.Command == "BATCH" {
		ref := m.Param(0)
		if len(ref) < 2 {
			return false
		}
		switch ref[0] {
		case '+':
			if m.Param(1) != "draft/multiline" {
				return false
			}
			tags := map[string]string{}
			for k, v := range m.Tags {
				tags[k] = v
			}
			c.mu.Lock()
			if c.mlBatches == nil {
				c.mlBatches = map[string]*multilineBatch{}
			}
			if len(c.mlBatches) >= maxOpenMultiline {
				c.mu.Unlock()
				return true // too many unfinished: ignore (each holds up to 64 KB)
			}
			c.mlBatches[ref[1:]] = &multilineBatch{tags: tags, target: m.Param(2)}
			c.mu.Unlock()
			return true
		case '-':
			c.mu.Lock()
			mb := c.mlBatches[ref[1:]]
			delete(c.mlBatches, ref[1:])
			c.mu.Unlock()
			if mb == nil {
				return false
			}
			if mb.lines > 0 {
				cmd := mb.cmd
				if cmd == "" {
					cmd = "PRIVMSG"
				}
				c.handle(&Message{Tags: mb.tags, Prefix: mb.prefix, Command: cmd, Params: []string{mb.target, mb.text.String()}})
			}
			return true
		}
		return false
	}
	ref := m.Tags["batch"]
	if ref == "" || (m.Command != "PRIVMSG" && m.Command != "NOTICE") {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	mb := c.mlBatches[ref]
	if mb == nil {
		return false
	}
	if mb.text.Len() > 64*1024 {
		return true // runaway batch: ignore the rest
	}
	if mb.lines > 0 {
		if _, concat := m.Tags["draft/multiline-concat"]; !concat {
			mb.text.WriteByte('\n')
		}
	}
	mb.text.WriteString(m.Trailing())
	mb.lines++
	if mb.prefix == "" {
		mb.prefix, mb.cmd = m.Prefix, m.Command
	}
	return true
}

// pastedLines returns the lines of a multi-line message (blank lines at the ends dropped),
// or nil when it's a single line or contains commands, which run one by one as usual.
func pastedLines(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 2 {
		return nil
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "/") {
			return nil
		}
	}
	return lines
}

// splitExact cuts s into pieces of at most max bytes that join back to exactly s (multiline
// "concat" pieces), preferring to cut just after a space and never inside a UTF-8 character.
func splitExact(s string, max int) []string {
	if max < 16 {
		max = 16
	}
	var out []string
	for len(s) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if sp := strings.LastIndexByte(s[:cut], ' '); sp > max/2 {
			cut = sp + 1
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	return append(out, s)
}
