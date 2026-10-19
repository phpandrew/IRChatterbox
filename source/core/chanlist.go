package core

// Channel browser: the server's channel list (LIST), collected and handed to the UI in
// batches instead of being printed line by line.

import (
	"strconv"
	"strings"
	"time"
)

type ChanListItem struct {
	Name  string `json:"name"`
	Users int    `json:"users"`
	Topic string `json:"topic"`
}

// ListChannels asks the server for its channels. filter is passed to LIST as-is when it
// looks like a mask ("*linux*") or ELIST condition (">50"); "" lists everything.
// Results arrive as "chanlist" UI events. Returns an error message or "".
func (a *App) ListChannels(server, filter string) string {
	c := a.conn(server)
	if c == nil || c.Status() != "connected" {
		return "Not connected"
	}
	c.startList(filter)
	return ""
}

func (c *Conn) startList(filter string) {
	filter = strings.TrimSpace(filter)
	if strings.ContainsAny(filter, "\r\n") {
		filter = ""
	}
	c.mu.Lock()
	c.listing, c.listActive = nil, true
	c.mu.Unlock()
	c.app.emitUI("chanlist", map[string]any{"server": c.cfg.ID, "items": []ChanListItem{}, "start": true})
	if filter != "" {
		c.SendRaw("LIST " + filter)
	} else {
		c.SendRaw("LIST")
	}
	// some servers never send 323 when they refuse LIST: give up after a while
	time.AfterFunc(2*time.Minute, func() {
		c.mu.Lock()
		stuck := c.listActive && len(c.listing) == 0
		if stuck {
			c.listActive = false
		}
		c.mu.Unlock()
		if stuck {
			c.app.emitUI("chanlist", map[string]any{"server": c.cfg.ID, "items": []ChanListItem{}, "done": true})
		}
	})
}

// onList collects 321/322/323 while the browser asked for them. Reports whether it took m.
func (c *Conn) onList(m *Message) bool {
	c.mu.Lock()
	if !c.listActive {
		c.mu.Unlock()
		return false
	}
	var batch []ChanListItem
	done := false
	switch m.Command {
	case "322":
		n, _ := strconv.Atoi(m.Param(2))
		topic := m.Trailing()
		if len(m.Params) < 4 {
			topic = ""
		}
		c.listing = append(c.listing, ChanListItem{Name: m.Param(1), Users: n, Topic: stripFormatting(topic)})
		if len(c.listing) >= 500 {
			batch, c.listing = c.listing, nil
		}
	case "323":
		batch, c.listing, c.listActive, done = c.listing, nil, false, true
	}
	c.mu.Unlock()
	if batch != nil || done {
		if batch == nil {
			batch = []ChanListItem{}
		}
		c.app.emitUI("chanlist", map[string]any{"server": c.cfg.ID, "items": batch, "done": done})
	}
	return true
}
