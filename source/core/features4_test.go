package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPastedLines(t *testing.T) {
	if got := pastedLines("\n\nfirst\n\n  second\n\n"); strings.Join(got, "|") != "first||  second" {
		t.Fatalf("%q", got)
	}
	for _, one := range []string{"single", "a\n/join #x", "\n\nonly\n"} {
		if pastedLines(one) != nil {
			t.Errorf("%q treated as multi-line", one)
		}
	}
}

func TestUploadPaste(t *testing.T) {
	var got string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		if strings.Contains(got, "huge") {
			w.WriteHeader(206)
			return
		}
		w.WriteHeader(201)
		io.WriteString(w, "https://paste.example/abc\n")
	}))
	defer srv.Close()
	old := pasteClient
	pasteClient = srv.Client()
	defer func() { pasteClient = old }()
	u, err := uploadPaste(srv.URL, "line 1\nline 2")
	if err != nil || u != "https://paste.example/abc" || got != "line 1\nline 2" {
		t.Fatalf("%q %v %q", u, err, got)
	}
	if _, err := uploadPaste(srv.URL, "huge"); err == nil {
		t.Fatal("truncated paste accepted")
	}
	a := newBackupApp(t)
	if r := a.UploadPaste("   "); r.Error == "" {
		t.Fatal("empty paste")
	}
	if r := a.UploadPaste(strings.Repeat("x", pasteMaxBytes+1)); r.Error == "" {
		t.Fatal("oversized paste")
	}
}

func TestSplitExact(t *testing.T) {
	for _, s := range []string{strings.Repeat("word ", 300), strings.Repeat("é", 500), "short", strings.Repeat("x", 1000)} {
		parts := splitExact(s, 100)
		if strings.Join(parts, "") != s {
			t.Fatalf("pieces don't join back for %q…", s[:10])
		}
		for _, p := range parts {
			if len(p) > 100 || !utf8.ValidString(p) {
				t.Fatalf("bad piece %q", p)
			}
		}
	}
}
