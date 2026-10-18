//go:build integration

package core

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A private chat that was open reopens (with its history) after a restart.
func TestQueriesSurviveRestart(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "q")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	id := newID()[:4]
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	a.store.Update(func(c *Config) { c.Servers = nil })
	sv := a.SaveServer(ServerConfig{Name: "Ergo", Host: "127.0.0.1", Port: 6667, Nick: "q" + id})
	a.Connect(sv.ID)
	log.waitFor(t, "connected", func(e Event) bool { return e.Type == "status" && e.Extra["status"] == "connected" })
	bob := dialRaw(t, "bob"+id)
	bob.send("PRIVMSG q" + id + " :hello there")
	log.waitFor(t, "pm", func(e Event) bool { return e.Type == "msg" && e.Text == "hello there" })
	time.Sleep(300 * time.Millisecond)
	a.Disconnect(sv.ID)
	time.Sleep(300 * time.Millisecond)

	// "restart": a new App on the same data folder
	b := NewApp()
	log2 := &evLog{}
	b.testHook = log2.add
	b.Connect(sv.ID)
	e := log2.waitFor(t, "query reopened", func(e Event) bool { return e.Type == "buffer" && e.Kind == "query" })
	if !strings.EqualFold(e.Buffer, "bob"+id) {
		t.Fatalf("reopened %q", e.Buffer)
	}
	if h := b.GetHistory(sv.ID, "bob"+id); len(h) == 0 || h[len(h)-1].Text != "hello there" {
		t.Fatalf("history %+v", h)
	}
	// closing it forgets it
	b.CloseBuffer(sv.ID, "bob"+id)
	c := NewApp()
	if c.hasQuery(sv.ID, "bob"+id) {
		t.Fatal("closed query came back")
	}
	b.Disconnect(sv.ID)
}
