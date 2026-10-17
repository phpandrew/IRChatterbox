//go:build integration

// Needs soju on 127.0.0.1:6698 (user andrew / secret, network "local" pointing at the ergo
// on 127.0.0.1:6667). Skipped when soju isn't running.

package core

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBouncer(t *testing.T) {
	if c, err := net.DialTimeout("tcp", "127.0.0.1:6698", time.Second); err != nil {
		t.Skip("soju not running")
	} else {
		c.Close()
	}
	// not t.TempDir(): the connection may still write its state as the test ends
	tmp, _ := os.MkdirTemp("", "bnc")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	a.store.Update(func(c *Config) { c.Servers = nil; c.Plugins.WeatherCooldown = 0; c.Settings.Logging = true })
	id := newID()[:4]
	ch := "#bnc" + id
	sv := a.SaveServer(ServerConfig{Name: "Bouncer", Host: "127.0.0.1", Port: 6698, Nick: "sojuandy",
		Bouncer: "soju", BouncerUser: "andrew", BouncerPass: "secret", BouncerNetwork: "local", AutoJoin: []string{ch}})

	a.Connect(sv.ID)
	log.waitFor(t, "connected", func(e Event) bool { return e.Type == "status" && e.Extra["status"] == "connected" })
	log.waitFor(t, "joined", func(e Event) bool { return e.Type == "buffer" && e.Buffer == ch })

	// close the app; the bouncer stays in the channel
	a.Disconnect(sv.ID)
	log.waitFor(t, "disconnected", func(e Event) bool { return e.Type == "status" && e.Extra["status"] == "disconnected" })

	bob := dialRaw(t, "bob"+newID()[:3])
	bob.send("JOIN " + ch)
	bob.waitFor(t, "366", 3*time.Second)
	bob.send("PRIVMSG " + ch + " :Today it feels like 20c " + id)
	bob.send("PRIVMSG " + ch + " :hey sojuandy are you there " + id)
	bob.send("PRIVMSG sojuandy :\x01VERSION\x01")
	bob.send("PRIVMSG sojuandy :private hello " + id)
	time.Sleep(800 * time.Millisecond)

	// reopen: missed lines come back as replay
	log.mu.Lock()
	reconnectAt := len(log.all)
	log.mu.Unlock()
	a.Connect(sv.ID)
	e := log.waitFor(t, "replayed weather line", func(e Event) bool { return e.Type == "msg" && strings.Contains(e.Text, "feels like 20c "+id) })
	if e.Extra["replay"] != true || e.Buffer != ch {
		t.Fatalf("expected a replayed line in %s, got %+v", ch, e)
	}
	e = log.waitFor(t, "replayed mention", func(e Event) bool { return e.Type == "msg" && strings.Contains(e.Text, "are you there "+id) })
	if !e.Highlight {
		t.Fatal("replayed mention should still be highlighted")
	}
	e = log.waitFor(t, "replayed PM", func(e Event) bool { return e.Type == "msg" && e.Text == "private hello "+id })
	if e.Extra["query"] != true || !strings.HasPrefix(e.Buffer, "bob") {
		t.Fatalf("PM should open a query: %+v", e)
	}
	log.waitFor(t, "summary", func(e Event) bool {
		return e.Type == "info" && strings.Contains(e.Text, "While you were away: 2 mentions and private messages (first in "+ch)
	})

	// nothing answered the old messages
	time.Sleep(1500 * time.Millisecond)
	bob.mu.Lock()
	for _, l := range bob.lines {
		if strings.Contains(l, "[Bot]") || strings.Contains(l, "VERSION IRChatterbox") {
			t.Fatalf("replayed history triggered a reply: %s", l)
		}
	}
	bob.mu.Unlock()

	// live messages still work normally (and the bot answers them)
	bob.send("PRIVMSG " + ch + " :now it's 25c")
	e = log.waitFor(t, "live line", func(e Event) bool { return e.Type == "msg" && strings.Contains(e.Text, "25c") })
	if e.Extra["replay"] == true {
		t.Fatal("live line marked as replay")
	}
	bob.waitFor(t, "[Bot] 25 C = 77 F", 4*time.Second)

	// a message I send from another device (my phone) reaches this app through the bouncer,
	// and the bots answer it just like something typed here
	phone := dialRawAt(t, "127.0.0.1:6698", "sojuandy", "andrew/local", "secret")
	phone.send("PRIVMSG " + ch + " :phone says it's 68F " + id)
	e2 := log.waitFor(t, "phone line", func(e Event) bool { return e.Type == "msg" && strings.Contains(e.Text, "phone says") })
	if !e2.Self || e2.Extra["replay"] == true {
		t.Fatalf("phone line should be a live line from me: %+v", e2)
	}
	bob.waitFor(t, "[Bot] 68 F = 20 C", 4*time.Second)
	phone.c.Close()

	// reconnecting doesn't show "you joined" again for channels the bouncer was already in
	log.mu.Lock()
	for _, ev := range log.all[reconnectAt:] {
		if ev.Type == "event" && ev.Kind == "join" && ev.Self {
			log.mu.Unlock()
			t.Fatalf("bouncer's re-sent JOIN shown as a join: %+v", ev)
		}
	}
	log.mu.Unlock()

	// read marker: mark as read, reconnect, the bouncer reports it back
	a.MarkRead(sv.ID, ch, e.Time)
	time.Sleep(500 * time.Millisecond)
	a.Disconnect(sv.ID)
	log.waitFor(t, "disconnected 2", func(e Event) bool { return e.Type == "status" && e.Extra["status"] == "disconnected" })
	log.mu.Lock()
	log.evs = nil
	log.mu.Unlock()
	a.Connect(sv.ID)
	r := log.waitFor(t, "read marker", func(e Event) bool { return e.Type == "read" && strings.EqualFold(e.Buffer, ch) })
	if r.Time < e.Time {
		t.Fatalf("read marker %d older than %d", r.Time, e.Time)
	}
	// and nothing is replayed twice
	time.Sleep(2500 * time.Millisecond)
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, ev := range log.evs {
		if ev.Type == "msg" && ev.Extra["replay"] == true {
			t.Fatalf("replayed again after reconnect: %+v", ev)
		}
		if ev.Type == "info" && strings.Contains(ev.Text, "While you were away") {
			t.Fatal("summary shown again with nothing new")
		}
	}
	a.SendInput(sv.ID, ch, "/part")
	time.Sleep(300 * time.Millisecond)
	a.Disconnect(sv.ID)
	time.Sleep(time.Second) // let the connection finish writing its state before TempDir cleanup
}
