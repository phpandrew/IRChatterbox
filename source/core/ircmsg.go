package core

import (
	"strings"
	"time"
)

// Message is a parsed IRC line.
type Message struct {
	Tags    map[string]string
	Prefix  string
	Command string
	Params  []string
}

func (m *Message) Nick() string {
	if i := strings.IndexByte(m.Prefix, '!'); i >= 0 {
		return m.Prefix[:i]
	}
	return m.Prefix
}

func (m *Message) UserHost() string {
	if i := strings.IndexByte(m.Prefix, '!'); i >= 0 {
		return m.Prefix[i+1:]
	}
	return ""
}

func (m *Message) Param(i int) string {
	if i < len(m.Params) {
		return m.Params[i]
	}
	return ""
}

// From returns the parameters from index i on (none if there aren't that many).
func (m *Message) From(i int) []string {
	if i < len(m.Params) {
		return m.Params[i:]
	}
	return nil
}

func (m *Message) Trailing() string {
	if len(m.Params) == 0 {
		return ""
	}
	return m.Params[len(m.Params)-1]
}

func (m *Message) Time() time.Time {
	if t, ok := m.Tags["time"]; ok {
		if pt, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return pt
		}
	}
	return time.Now()
}

var tagUnescape = strings.NewReplacer(`\:`, ";", `\s`, " ", `\\`, `\`, `\r`, "\r", `\n`, "\n")

func ParseMessage(line string) *Message {
	line = strings.TrimRight(line, "\r\n")
	m := &Message{Tags: map[string]string{}}
	if strings.HasPrefix(line, "@") {
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			return m
		}
		for _, kv := range strings.Split(line[1:sp], ";") {
			k, v, _ := strings.Cut(kv, "=")
			m.Tags[k] = tagUnescape.Replace(v)
		}
		line = strings.TrimLeft(line[sp+1:], " ")
	}
	if strings.HasPrefix(line, ":") {
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			m.Prefix = line[1:]
			return m
		}
		m.Prefix = line[1:sp]
		line = strings.TrimLeft(line[sp+1:], " ")
	}
	for len(line) > 0 {
		if line[0] == ':' {
			m.Params = append(m.Params, line[1:])
			break
		}
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			m.Params = append(m.Params, line)
			break
		}
		m.Params = append(m.Params, line[:sp])
		line = strings.TrimLeft(line[sp+1:], " ")
	}
	if len(m.Params) > 0 {
		m.Command = strings.ToUpper(m.Params[0])
		m.Params = m.Params[1:]
	}
	return m
}

// splitText splits a message into chunks of at most max bytes, preferring word boundaries
// and never splitting a UTF-8 character.
func splitText(s string, max int) []string {
	var out []string
	for len(s) > max {
		cut := max
		for cut > 0 && (s[cut]&0xC0) == 0x80 {
			cut--
		}
		if sp := strings.LastIndexByte(s[:cut], ' '); sp > max/2 {
			cut = sp
		}
		out = append(out, s[:cut])
		s = strings.TrimLeft(s[cut:], " ")
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}
