package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Lowering "Maximum saved lines" trims the app's own logs and nothing else in a folder the user
// picked for text logs (say, Documents).
func TestTrimAllLogsLeavesOtherFilesAlone(t *testing.T) {
	a := newBackupApp(t)
	sv := a.SaveServer(ServerConfig{Name: "x", Host: "irc.example.org", Port: 6697, Nick: "me"})
	docs := t.TempDir()
	a.store.Update(func(c *Config) { c.Settings.LogFolder, c.Settings.TextLogs, c.Settings.LogMaxLines = docs, true, 5 })
	long := strings.Repeat("line\n", 50)
	own := filepath.Join(docs, safeName(sv.ID), "#chan.txt")
	os.MkdirAll(filepath.Dir(own), 0o700)
	os.WriteFile(own, []byte(long), 0o600)
	other := filepath.Join(docs, "thesis-notes.txt")
	os.WriteFile(other, []byte(long), 0o600)
	stray := filepath.Join(docs, "some-other-folder", "list.txt")
	os.MkdirAll(filepath.Dir(stray), 0o700)
	os.WriteFile(stray, []byte(long), 0o600)

	a.TrimAllLogs()
	if b, _ := os.ReadFile(own); strings.Count(string(b), "\n") > 5 {
		t.Fatalf("own log not trimmed: %d lines", strings.Count(string(b), "\n"))
	}
	for _, p := range []string{other, stray} {
		if b, _ := os.ReadFile(p); string(b) != long {
			t.Fatalf("%s was changed", p)
		}
	}
}
