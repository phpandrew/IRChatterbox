package core

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWordFilter(t *testing.T) {
	re := filterRegexp([]string{"darn", "heck*", " ", "c.a.t"})
	cases := map[string]string{
		"darn it":            "•••• it",
		"DARN, darn darn!":   "••••, •••• ••••!",
		"darning needle":     "darning needle", // whole words only
		"what the heckin no": "what the •••••• no",
		"c.a.t and cat":      "••••• and cat", // dots are literal
		"no match here":      "no match here",
		"über darn émoji":    "über •••• émoji",
	}
	for in, want := range cases {
		if got := maskWords(re, in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	if filterRegexp(nil) != nil || maskWords(nil, "darn") != "darn" {
		t.Error("an empty list must not filter")
	}
}

func TestPostReport(t *testing.T) {
	var got map[string]any
	var install string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		install = r.Header.Get("X-IRChatterbox-Install")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		if got["reason"] == "spam" {
			w.Write([]byte(`{"ok":true}`))
		} else {
			w.WriteHeader(429)
			w.Write([]byte(`{"ok":false,"error":"Too many reports, try later"}`))
		}
	}))
	defer srv.Close()
	old := reportURL
	reportURL = srv.URL
	defer func() { reportURL = old }()

	a := &App{store: &Store{cfg: defaultConfig()}}
	a.store.cfg.InstallID = strings.Repeat("ab", 16)
	if msg := a.SendReport(ReportInput{Nick: "bob", Reason: "spam", Message: "buy now", Context: []string{"<bob> buy now"}}); msg != "" {
		t.Fatalf("send: %s", msg)
	}
	if got["nick"] != "bob" || got["message"] != "buy now" || install != strings.Repeat("ab", 16) {
		t.Fatalf("payload %v install %q", got, install)
	}
	if msg := a.SendReport(ReportInput{Nick: "bob", Reason: "other"}); !strings.Contains(msg, "Too many") {
		t.Fatalf("server error not shown: %q", msg)
	}
	if msg := a.SendReport(ReportInput{Nick: "bob", Reason: "bogus"}); msg == "" {
		t.Fatal("bad reason accepted")
	}
}
