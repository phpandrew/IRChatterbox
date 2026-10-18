package core

// Open private chats are remembered in queries.json and reopened, with their history, when
// the server connects, the same way channels are rejoined. Closing one forgets it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

func (a *App) queriesPath() string { return filepath.Join(a.store.dir, "queries.json") }

func (a *App) loadQueries() {
	b, err := os.ReadFile(a.queriesPath())
	if err != nil {
		return
	}
	var saved map[string][]string
	if json.Unmarshal(b, &saved) != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for server, nicks := range saved {
		m := map[string]string{}
		for _, n := range nicks {
			if n != "" {
				m[lower(n)] = n
			}
		}
		a.queries[server] = m
	}
}

func (a *App) saveQueries() {
	a.mu.Lock()
	out := map[string][]string{}
	for server, m := range a.queries {
		for _, n := range m {
			out[server] = append(out[server], n)
		}
		sort.Strings(out[server])
	}
	a.mu.Unlock()
	b, _ := json.MarshalIndent(out, "", "  ")
	tmp := a.queriesPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, a.queriesPath())
	}
}

// restoreQueries reopens the private chats that were open last time on this server.
func (a *App) restoreQueries(c *Conn) {
	a.mu.Lock()
	var nicks []string
	for _, n := range a.queries[c.cfg.ID] {
		nicks = append(nicks, n)
	}
	a.mu.Unlock()
	sort.Strings(nicks)
	for _, n := range nicks {
		c.emit(Event{Type: "buffer", Buffer: n, Kind: "query", Extra: map[string]any{"action": "open"}})
	}
}
