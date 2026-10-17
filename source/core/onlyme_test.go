//go:build integration

package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestOnlyMe(t *testing.T) {
	tmp := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmp)
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("solar 4kW")) }))
	defer hs.Close()
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	if !a.store.Get().Plugins.URLCommands[0].OnlyMe {
		t.Fatal("default !solar should be OnlyMe")
	}
	a.store.Update(func(c *Config) {
		c.Servers = nil
		c.Plugins.URLCommands[0].URL = hs.URL
		c.Plugins.URLCommands[0].Cooldown = 0
	})
	sv := a.SaveServer(ServerConfig{Name: "L", Host: "127.0.0.1", Port: 6667, Nick: "Andy" + newID()[:3], AutoJoin: []string{"#solar"}})
	a.Connect(sv.ID)
	log.waitFor(t, "join", func(e Event) bool { return e.Type == "buffer" && e.Buffer == "#solar" })
	bob := dialRaw(t, "bob"+newID()[:3])
	bob.send("JOIN #solar")
	bob.waitFor(t, "366", 3*time.Second)
	bob.send("PRIVMSG #solar :!solar")
	time.Sleep(1500 * time.Millisecond)
	bob.mu.Lock()
	for _, l := range bob.lines {
		if contains(l, "solar 4kW") {
			t.Fatal("bot answered someone else")
		}
	}
	bob.mu.Unlock()
	a.SendInput(sv.ID, "#solar", "!solar")
	bob.waitFor(t, "[Bot] solar 4kW", 4*time.Second)
}

func contains(a, b string) bool {
	return len(a) >= len(b) && (func() bool {
		for i := 0; i+len(b) <= len(a); i++ {
			if a[i:i+len(b)] == b {
				return true
			}
		}
		return false
	})()
}
