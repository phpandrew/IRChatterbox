package core

// Bouncer support: history playback (IRCv3 chathistory, or ZNC's buffer playback) and read
// markers (draft/read-marker) shared with your other devices.
//
// Missed messages arrive as "replay": they're shown and logged like normal lines, but never
// trigger bots, CTCP replies or one toast each. Instead a single summary notification says
// how many mentions you missed.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type selfLine struct {
	t    int64
	text string
}

const isoMilli = "2006-01-02T15:04:05.000Z"

func isoTime(ms int64) string { return time.UnixMilli(ms).UTC().Format(isoMilli) }

// isReplay reports whether m is history rather than something happening now.
func (c *Conn) isReplay(m *Message) bool {
	if ref := m.Tags["batch"]; ref != "" {
		c.mu.Lock()
		typ := c.batches[ref]
		c.mu.Unlock()
		if typ == "chathistory" || typ == "znc.in/playback" {
			return true
		}
	}
	// ZNC without the batch capability: playback is only recognisable by its old timestamps
	if c.cfg.Bouncer != "" && m.Tags["time"] != "" && m.Command != "BATCH" {
		c.mu.Lock()
		since := c.connectedAt
		c.mu.Unlock()
		return m.Time().Before(since.Add(-15 * time.Second))
	}
	return false
}

func (c *Conn) onBatch(m *Message) {
	ref := m.Param(0)
	if len(ref) < 2 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch ref[0] {
	case '+':
		c.batches[ref[1:]] = m.Param(1)
	case '-':
		typ := c.batches[ref[1:]]
		delete(c.batches, ref[1:])
		if typ == "chathistory" || typ == "znc.in/playback" {
			c.scheduleSummaryLocked()
		}
	}
}

// fetchHistory asks the bouncer for what was said in target since we last saw it.
func (c *Conn) fetchHistory(target string) {
	if c.cfg.Bouncer == "" {
		return
	}
	c.mu.Lock()
	if !c.caps["draft/chathistory"] || c.histFetched[lower(target)] {
		c.mu.Unlock()
		return
	}
	c.histFetched[lower(target)] = true
	limit := c.historyLimit
	c.mu.Unlock()
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	last := c.lastSeenFor(target)
	c.mu.Lock()
	c.replayBase[lower(target)] = last
	c.mu.Unlock()
	if last > 0 {
		// newest messages after the last one we have (up to limit)
		c.SendRaw("CHATHISTORY LATEST " + target + " timestamp=" + isoTime(last) + " " + itoa(limit))
	} else {
		c.SendRaw("CHATHISTORY LATEST " + target + " * " + itoa(min(limit, 100)))
	}
}

// fetchMissedQueries asks which private conversations had activity recently, so PMs you
// got while offline open as queries. Channels are fetched as the bouncer re-joins them.
func (c *Conn) fetchMissedQueries() {
	go func() {
		time.Sleep(1500 * time.Millisecond)
		c.mu.Lock()
		ok := c.caps["draft/chathistory"]
		c.mu.Unlock()
		if !ok {
			return
		}
		from := time.Now().Add(-14 * 24 * time.Hour).UnixMilli()
		to := time.Now().Add(24 * time.Hour).UnixMilli()
		// newest first (the later timestamp goes first), so a busy past doesn't crowd out
		// the conversations that matter: the 50 most recently active
		c.SendRaw("CHATHISTORY TARGETS timestamp=" + isoTime(to) + " timestamp=" + isoTime(from) + " 50")
	}()
}

// onHistoryTargets handles "CHATHISTORY TARGETS <target> <time>" lines.
func (c *Conn) onHistoryTargets(m *Message) {
	if !strings.EqualFold(m.Param(0), "TARGETS") {
		return
	}
	target := m.Param(1)
	if target == "" || c.isChannel(target) || strings.EqualFold(target, c.Nick()) {
		return
	}
	at, err := time.Parse(time.RFC3339Nano, m.Param(2))
	if err == nil && at.UnixMilli() <= c.lastSeenFor(target) {
		return // nothing new in that conversation
	}
	c.fetchHistory(target)
}

// onReplayMessage shows a message from history: no CTCP replies, no bots, no per-line toasts.
func (c *Conn) onReplayMessage(m *Message, nick string, isMe bool, ts int64) {
	target := m.Param(0)
	text := m.Trailing()
	kind := strings.ToLower(m.Command)
	if m.Command == "NOTICE" && !strings.Contains(m.Prefix, "!") {
		return // old server notices aren't worth replaying
	}
	if c.app.isIgnored(nick, m.Prefix) {
		return
	}
	if len(text) > 1 && text[0] == 1 {
		body := strings.Trim(text, "\x01")
		cmd, arg, _ := strings.Cut(body, " ")
		if !strings.EqualFold(cmd, "ACTION") {
			return // never answer an old CTCP request
		}
		kind, text = "action", arg
	}
	isPM := !c.isChannel(target)
	buffer := target
	if isPM && !isMe {
		buffer = nick
	}
	if c.alreadyHave(buffer, ts, isMe, text) {
		return
	}
	highlight := !isMe && c.app.isHighlight(c.Nick(), text)
	extra := msgExtra(m)
	extra["replay"] = true
	if isPM {
		extra["query"] = true
	}
	c.emit(Event{Type: "msg", Buffer: buffer, Kind: kind, Nick: nick, Prefix: c.prefixOf(target, nick), Text: text, Time: ts, Self: isMe, Highlight: highlight, Extra: extra})
	c.noteSeen(buffer, ts, isMe, text)

	if !isMe && (highlight || (isPM && kind != "notice")) {
		c.mu.Lock()
		// only count what arrived after you last looked: after the read marker, and after this
		// app was last connected (the first ever connection just fills in history quietly)
		since := max(c.readMarks[lower(buffer)], c.prevOnline)
		if since > 0 && ts > since {
			if c.replayMentions == 0 {
				c.replayFirst = buffer
			}
			c.replayMentions++
		}
		c.scheduleSummaryLocked()
		c.mu.Unlock()
	}
}

// scheduleSummaryLocked shows one "N mentions while you were away" notification once the
// history has finished arriving. c.mu must be held.
func (c *Conn) scheduleSummaryLocked() {
	if c.summaryTimer != nil {
		c.summaryTimer.Stop()
	}
	c.summaryTimer = time.AfterFunc(2*time.Second, func() {
		c.mu.Lock()
		n, buf := c.replayMentions, c.replayFirst
		c.replayMentions, c.replayFirst = 0, ""
		c.mu.Unlock()
		if n > 0 {
			what := "1 mention or private message"
			if n > 1 {
				what = itoa(n) + " mentions and private messages"
			}
			c.info("", "highlight", "While you were away: "+what+" (first in "+buf+")")
			c.app.notifyMissed(c, buf, n)
		}
	})
}

// ---------- what we already have ----------

// noteSeen records a message we've shown, so the next history fetch starts after it and
// replayed copies are skipped. Our own lines are stamped with the local clock, which may
// differ from the server's, so they're matched by text instead of time.
func (c *Conn) noteSeen(buffer string, ts int64, self bool, text string) {
	if buffer == "" || buffer == "*active*" {
		return
	}
	k := lower(buffer)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastSeen == nil {
		return
	}
	c.loadSeenLocked(buffer)
	if self {
		l := append(c.recentSelf[k], selfLine{ts, text})
		if len(l) > 50 {
			l = l[len(l)-50:]
		}
		c.recentSelf[k] = l
	} else if ts > c.lastSeen[k] {
		c.lastSeen[k] = ts
	}
}

func (c *Conn) lastSeenFor(buffer string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastSeen == nil {
		return 0
	}
	c.loadSeenLocked(buffer)
	return c.lastSeen[lower(buffer)]
}

// alreadyHave reports whether a replayed message is one we've shown or logged before.
func (c *Conn) alreadyHave(buffer string, ts int64, self bool, text string) bool {
	k := lower(buffer)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadSeenLocked(buffer)
	if self {
		for _, s := range c.recentSelf[k] {
			if s.text == text && abs64(s.t-ts) < 120_000 {
				return true
			}
		}
		return false
	}
	// compare with what we had when the history started arriving, not with live messages
	// that may have come in since (those are newer than everything being replayed)
	base, ok := c.replayBase[k]
	if !ok {
		base = c.lastSeen[k]
		c.replayBase[k] = base
	}
	return ts <= base
}

// loadSeenLocked seeds lastSeen/recentSelf for a buffer from its history log, the first time
// the buffer is touched on this connection. c.mu must be held.
func (c *Conn) loadSeenLocked(buffer string) {
	k := lower(buffer)
	if _, ok := c.lastSeen[k]; ok {
		return
	}
	c.lastSeen[k] = 0
	for _, ev := range c.app.historyTail(c.cfg.Name, buffer, 300) {
		if ev.Type != "msg" {
			continue
		}
		if ev.Self {
			c.recentSelf[k] = append(c.recentSelf[k], selfLine{ev.Time, ev.Text})
		} else if ev.Time > c.lastSeen[k] {
			c.lastSeen[k] = ev.Time
		}
	}
	if l := c.recentSelf[k]; len(l) > 50 {
		c.recentSelf[k] = l[len(l)-50:]
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func itoa(n int) string { return strconv.Itoa(n) }

// ---------- read markers ----------

// onMarkRead handles "MARKREAD <target> timestamp=…" from the bouncer: another device (or
// this one) has read up to that point.
func (c *Conn) onMarkRead(m *Message) {
	target := m.Param(0)
	v := m.Param(1)
	if !strings.HasPrefix(v, "timestamp=") {
		return // "*" = no marker yet
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(v, "timestamp="))
	if err != nil {
		return
	}
	ms := at.UnixMilli()
	c.mu.Lock()
	if ms < c.readMarks[lower(target)] {
		c.mu.Unlock()
		return
	}
	c.readMarks[lower(target)] = ms
	c.mu.Unlock()
	buffer := target
	c.emit(Event{Type: "read", Buffer: buffer, Time: ms})
}

// markRead tells the bouncer we've read target up to ms, so other devices clear it too.
func (c *Conn) markRead(target string, ms int64) {
	if target == "" || target == "*active*" {
		return
	}
	c.mu.Lock()
	ok := c.caps["draft/read-marker"] && c.status == "connected" && ms > c.readMarks[lower(target)]
	if ok {
		c.readMarks[lower(target)] = ms
	}
	c.mu.Unlock()
	if ok {
		c.SendRaw("MARKREAD " + target + " timestamp=" + isoTime(ms))
	}
}

// ---------- last online (per bouncer server) ----------

// lastOnline is when IRChatterbox was last connected to this bouncer, kept in bouncer.json so
// "while you were away" only counts what came in after that.
func (a *App) lastOnline(serverID string) int64 {
	a.bncMu.Lock()
	defer a.bncMu.Unlock()
	a.loadBouncerStateLocked()
	return a.bncOnline[serverID]
}

func (a *App) setLastOnline(serverID string, ms int64) {
	a.bncMu.Lock()
	defer a.bncMu.Unlock()
	a.loadBouncerStateLocked()
	a.bncOnline[serverID] = ms
	if b, err := json.Marshal(a.bncOnline); err == nil {
		os.WriteFile(filepath.Join(a.store.dir, "bouncer.json"), b, 0o600)
	}
}

func (a *App) loadBouncerStateLocked() {
	if a.bncOnline != nil {
		return
	}
	a.bncOnline = map[string]int64{}
	if b, err := os.ReadFile(filepath.Join(a.store.dir, "bouncer.json")); err == nil {
		json.Unmarshal(b, &a.bncOnline)
	}
}

// inBouncerSync reports whether we're in the first seconds after connecting to a bouncer,
// while it re-sends the state of channels we're already in.
func (c *Conn) inBouncerSync() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.onlineSince.IsZero() && time.Since(c.onlineSince) < 15*time.Second
}
