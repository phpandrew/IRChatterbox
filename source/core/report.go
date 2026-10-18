package core

// Report: someone abusive or a message that breaks the rules can be reported from its
// right-click (long-press) menu. The report, with a few lines of context, goes to the
// IRChatterbox report endpoint for review; the app also blocks the person if asked.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

var reportURL = "https://i.irchatterbox.com/report"

var reportReasons = map[string]bool{"spam": true, "harassment": true, "hate": true, "sexual": true, "illegal": true, "other": true}

// ReportInput is what the UI sends: who, where, why, and the lines around it.
type ReportInput struct {
	Server  string   `json:"server"`
	Buffer  string   `json:"buffer"`
	Nick    string   `json:"nick"`
	Reason  string   `json:"reason"`
	Note    string   `json:"note"`
	Message string   `json:"message"`
	Context []string `json:"context"`
}

func clipText(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// SendReport sends a report. Returns "" on success or a message to show.
func (a *App) SendReport(r ReportInput) string {
	if !reportReasons[r.Reason] {
		return "Pick a reason"
	}
	if strings.TrimSpace(r.Nick) == "" {
		return "Nothing to report"
	}
	network, host, me := "", "", ""
	if c := a.conn(r.Server); c != nil {
		host, me = c.cfg.Host, c.Nick()
		c.mu.Lock()
		network = c.network
		c.mu.Unlock()
		if network == "" {
			network = c.cfg.Name
		}
	}
	ctx := r.Context
	if len(ctx) > 20 {
		ctx = ctx[len(ctx)-20:]
	}
	for i := range ctx {
		ctx[i] = clipText(ctx[i], 600)
	}
	body, _ := json.Marshal(map[string]any{
		"reason": r.Reason, "note": clipText(r.Note, 1000),
		"nick": clipText(r.Nick, 64), "channel": clipText(r.Buffer, 100), "message": clipText(r.Message, 1000), "context": ctx,
		"network": clipText(network, 100), "server": clipText(host, 200), "reporter": clipText(me, 64),
		"app": appVersion, "os": runtime.GOOS, "time": time.Now().UTC().Format(time.RFC3339),
	})
	if err := postReport(reportURL, a.store.Get().InstallID, body); err != nil {
		return err.Error()
	}
	return ""
}

func postReport(endpoint, installID string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("The report couldn't be sent")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "IRChatterbox/"+appVersion)
	if installID != "" {
		req.Header.Set("X-IRChatterbox-Install", installID)
	}
	resp, err := uploadClient.Do(req)
	if err != nil {
		return errors.New("Couldn't reach the report server. Check your connection and try again.")
	}
	defer resp.Body.Close()
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if json.Unmarshal(b, &res) != nil {
		return errors.New("The report server gave an unexpected reply")
	}
	if !res.OK {
		if res.Error != "" {
			return errors.New(res.Error)
		}
		return errors.New("The report couldn't be sent")
	}
	return nil
}
