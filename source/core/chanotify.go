package core

// Per-channel notification settings: every message, mentions only (the default for
// channels), or nothing — and muting for a while.

import (
	"strings"
	"time"
)

// NotifyRule is one buffer's setting. Level: "all" | "mentions" | "mute" ("" = default:
// mentions in channels, everything in private chats). Until: for "mute", when it ends
// (Unix ms; 0 = until turned back on).
type NotifyRule struct {
	Level string `json:"level"`
	Until int64  `json:"until,omitempty"`
}

func notifyKey(server, buffer string) string { return server + "/" + lower(buffer) }

// notifyLevel is the setting in force for a buffer: "all", "mentions" or "mute".
func (a *App) notifyLevel(server, buffer string, isPM bool) string {
	r, ok := a.store.Get().ChannelNotify[notifyKey(server, buffer)]
	if ok && r.Level == "mute" && r.Until > 0 && time.Now().UnixMilli() >= r.Until {
		ok = false // the mute has run out
	}
	if !ok || r.Level == "" {
		if isPM {
			return "all"
		}
		return "mentions"
	}
	return r.Level
}

// wantsNotify decides whether a message from someone else should notify you.
func (a *App) wantsNotify(c *Conn, buffer, kind string, isPM, highlight bool) bool {
	switch a.notifyLevel(c.cfg.ID, buffer, isPM) {
	case "mute":
		return false
	case "all":
		return highlight || kind != "notice"
	}
	return highlight || (isPM && kind != "notice")
}

// SetChannelNotify sets a buffer's notifications: level "all" | "mentions" | "mute" | ""
// (back to the default). minutes > 0 with "mute" mutes for that long.
func (a *App) SetChannelNotify(server, buffer, level string, minutes int) {
	if server == "" || buffer == "" {
		return
	}
	switch level {
	case "", "all", "mentions", "mute":
	default:
		return
	}
	a.store.Update(func(c *Config) {
		if c.ChannelNotify == nil {
			c.ChannelNotify = map[string]NotifyRule{}
		}
		k := notifyKey(server, buffer)
		if level == "" {
			delete(c.ChannelNotify, k)
			return
		}
		r := NotifyRule{Level: level}
		if level == "mute" && minutes > 0 {
			r.Until = time.Now().Add(time.Duration(minutes) * time.Minute).UnixMilli()
		}
		c.ChannelNotify[k] = r
	})
	a.emitUI("config", a.store.Get())
}

// pruneNotifyRules drops mutes that have run out, so the list doesn't grow forever.
func (a *App) pruneNotifyRules() {
	now := time.Now().UnixMilli()
	changed := false
	for k, r := range a.store.Get().ChannelNotify {
		if r.Level == "mute" && r.Until > 0 && now >= r.Until || strings.TrimSpace(k) == "" {
			changed = true
		}
	}
	if !changed {
		return
	}
	a.store.Update(func(c *Config) {
		for k, r := range c.ChannelNotify {
			if r.Level == "mute" && r.Until > 0 && now >= r.Until {
				delete(c.ChannelNotify, k)
			}
		}
	})
}
