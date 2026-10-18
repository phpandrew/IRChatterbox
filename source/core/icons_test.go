package core

import (
	"encoding/base64"
	"os"
	"testing"
)

func TestChannelIcons(t *testing.T) {
	dir := t.TempDir()
	a := &App{store: &Store{dir: dir, cfg: defaultConfig()}}
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG fake"))
	if msg := a.SetChannelIcon("s1", "#Linux", png); msg != "" {
		t.Fatal(msg)
	}
	if got := a.GetChannelIcons()["s1/#linux"]; got != png {
		t.Fatalf("got %q", got)
	}
	if msg := a.SetChannelIcon("s1", "#x", "data:text/html;base64,PHNjcmlwdD4="); msg == "" {
		t.Fatal("html accepted as an icon")
	}
	big := "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, maxIconBytes+1))
	if msg := a.SetChannelIcon("s1", "#x", big); msg == "" {
		t.Fatal("oversized icon accepted")
	}
	a.SetChannelIcon("s1", "#LINUX", "")
	if len(a.GetChannelIcons()) != 0 {
		t.Fatal("icon not removed")
	}
	if _, err := os.Stat(dir + "/icons.json"); err != nil {
		t.Fatal(err)
	}
}
