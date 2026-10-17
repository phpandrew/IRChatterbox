//go:build integration

package core

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Against ergo on 127.0.0.1:6667, which supports echo-message, labeled-response, setname,
// MONITOR and +typing.
func TestIRCv3(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "v3")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	id := newID()[:4]
	ch := "#v3" + id
	friend := "fr" + id
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	a.store.Update(func(c *Config) {
		c.Servers = nil
		c.Plugins.WeatherCooldown = 0
		c.Settings.NotifyList = []string{friend}
		c.Settings.SendTyping = true
	})
	sv := a.SaveServer(ServerConfig{Name: "Ergo", Host: "127.0.0.1", Port: 6667, Nick: "v3" + id, AutoJoin: []string{ch}})
	a.Connect(sv.ID)
	log.waitFor(t, "joined", func(e Event) bool { return e.Type == "buffer" && e.Buffer == ch })
	c := a.conn(sv.ID)
	for _, cp := range []string{"echo-message", "labeled-response", "setname", "message-tags"} {
		if !c.hasCap(cp) {
			t.Fatalf("cap %s not negotiated", cp)
		}
	}

	// echo-message: shown once, with the server's msgid
	a.SendInput(sv.ID, ch, "hello echo "+id)
	e := log.waitFor(t, "own echo", func(e Event) bool { return e.Type == "msg" && e.Text == "hello echo "+id })
	if !e.Self || e.Extra["msgid"] == nil {
		t.Fatalf("echo should be self with msgid: %+v", e)
	}
	time.Sleep(500 * time.Millisecond)
	log.mu.Lock()
	n := 0
	for _, x := range log.all {
		if x.Type == "msg" && x.Text == "hello echo "+id {
			n++
		}
	}
	log.mu.Unlock()
	if n != 1 {
		t.Fatalf("own message shown %d times", n)
	}

	// the bot still answers others, exactly once, and doesn't answer its own echoed reply
	bob := dialRaw(t, "bob"+id)
	bob.send("CAP REQ :message-tags")
	bob.send("JOIN " + ch)
	bob.waitFor(t, "366", 3*time.Second)
	bob.send("PRIVMSG " + ch + " :it feels like 20c")
	bob.waitFor(t, "[Bot] 20 C = 68 F", 4*time.Second)
	time.Sleep(1500 * time.Millisecond)
	bob.mu.Lock()
	for _, l := range bob.lines {
		if strings.Contains(l, "[Bot]") {
			bob.mu.Unlock()
			t.Fatalf("bot replied twice: %s", l)
		}
	}
	bob.mu.Unlock()

	// typing, both ways
	bob.send("@+typing=active TAGMSG " + ch)
	e = log.waitFor(t, "typing in", func(e Event) bool { return e.Type == "typing" && e.Buffer == ch })
	if e.Text != "active" || !strings.HasPrefix(e.Nick, "bob") {
		t.Fatalf("typing event %+v", e)
	}
	a.SendTyping(sv.ID, ch, "active")
	bob.waitFor(t, "+typing=active", 3*time.Second)

	// MONITOR: the friend comes online
	fr := dialRaw(t, friend)
	defer fr.c.Close()
	log.waitFor(t, "friend online", func(e Event) bool { return e.Type == "info" && strings.Contains(e.Text, friend+" is now online") })

	// setname
	a.SendInput(sv.ID, ch, "/setname Changed Name")
	log.waitFor(t, "setname", func(e Event) bool { return e.Type == "info" && strings.Contains(e.Text, "Changed Name") })

	a.Disconnect(sv.ID)
	time.Sleep(500 * time.Millisecond)
}
