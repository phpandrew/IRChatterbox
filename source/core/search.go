package core

// Searching all your chat history: every channel and private chat on every server, from
// the history logs (logs/<network>/<buffer>.jsonl).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type SearchHit struct {
	Event
	ServerName string `json:"serverName"`
}

type SearchResult struct {
	Hits    []SearchHit `json:"hits"`
	More    bool        `json:"more"` // there were more matches than shown
	Scanned int         `json:"scanned"`
}

const searchMax = 300

// SearchHistory finds messages containing every word of query (any capitalisation).
// server and buffer narrow it down ("" = all). Newest first.
func (a *App) SearchHistory(query, server, buffer string) SearchResult {
	words := strings.Fields(strings.ToLower(query))
	res := SearchResult{Hits: []SearchHit{}}
	if len(words) == 0 || utf8.RuneCountInString(query) > 200 {
		return res
	}
	names := map[string]string{} // server ID -> name
	for _, sv := range a.store.Get().Servers {
		names[sv.ID] = sv.Name
	}
	root := filepath.Join(a.store.dir, "logs")
	var files []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
			files = append(files, p)
		}
		return nil
	})
	if server != "" {
		if n, ok := names[server]; ok {
			dir := filepath.Join(root, safeName(n)) + string(filepath.Separator)
			keep := files[:0]
			for _, f := range files {
				if strings.HasPrefix(f, dir) {
					keep = append(keep, f)
				}
			}
			files = keep
		}
	}
	if buffer != "" {
		want := safeName(lower(buffer)) + ".jsonl"
		keep := files[:0]
		for _, f := range files {
			if filepath.Base(f) == want {
				keep = append(keep, f)
			}
		}
		files = keep
	}
	needles := make([][]byte, len(words))
	for i, w := range words {
		needles[i] = []byte(w)
	}
	var hits []SearchHit
	total := 0
	for _, f := range files {
		// read each file whole under the log lock (short), then search it without holding up logging
		a.logMu.Lock()
		data, err := os.ReadFile(f)
		a.logMu.Unlock()
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			res.Scanned++
			low := bytes.ToLower(sc.Bytes())
			ok := true
			for _, n := range needles {
				if !bytes.Contains(low, n) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			var ev Event
			if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Type != "msg" {
				continue
			}
			text := strings.ToLower(ev.Nick + " " + stripFormatting(ev.Text))
			match := true
			for _, w := range words {
				if !strings.Contains(text, w) {
					match = false
					break
				}
			}
			if !match || (server != "" && ev.Server != "" && ev.Server != server) {
				continue
			}
			total++
			if !ev.Self {
				ev.Text = a.maskFiltered(ev.Text)
			}
			hits = append(hits, SearchHit{Event: ev, ServerName: names[ev.Server]})
			if len(hits) > searchMax*4 { // keep memory bounded: drop the oldest
				sort.Slice(hits, func(i, j int) bool { return hits[i].Time > hits[j].Time })
				hits = hits[:searchMax]
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Time > hits[j].Time })
	if len(hits) > searchMax {
		hits = hits[:searchMax]
	}
	res.Hits = hits
	res.More = total > len(hits)
	return res
}

// HistoryAround returns up to n logged lines either side of the message at time t in a
// buffer, for showing a search result in context.
func (a *App) HistoryAround(server, buffer string, t int64, n int) []Event {
	sv, ok := a.store.Server(server)
	if !ok || n <= 0 || n > 200 {
		return []Event{}
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	fh, err := os.Open(a.logPath(sv.Name, buffer))
	if err != nil {
		return []Event{}
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var before []Event
	var after []Event
	found := false
	for sc.Scan() {
		var ev Event
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Type == "react" || ev.Type == "redact" {
			continue
		}
		if ev.Type == "msg" && !ev.Self {
			ev.Text = a.maskFiltered(ev.Text)
		}
		if !found {
			before = append(before, ev)
			if len(before) > n+1 {
				before = before[1:]
			}
			if ev.Time >= t {
				found = true
			}
			continue
		}
		after = append(after, ev)
		if len(after) >= n {
			break
		}
	}
	return append(before, after...)
}
