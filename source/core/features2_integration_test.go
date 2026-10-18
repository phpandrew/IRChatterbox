//go:build integration

package core

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ergoPair(t *testing.T, ch string) (a, b *App, la, lb *evLog, sa, sb string) {
	t.Helper()
	id := newID()[:4]
	mk := func(nick string) (*App, *evLog, string) {
		app := newBackupApp(t)
		l := &evLog{}
		app.testHook = l.add
		app.store.Update(func(c *Config) { c.Servers = nil; c.Settings.DCCAddress = "127.0.0.1" })
		sv := app.SaveServer(ServerConfig{Name: "Ergo", Host: "127.0.0.1", Port: 6667, Nick: nick + id, AutoJoin: []string{ch}})
		app.Connect(sv.ID)
		l.waitFor(t, nick+" joined", func(e Event) bool { return e.Type == "buffer" && e.Buffer == ch })
		return app, l, sv.ID
	}
	a, la, sa = mk("al")
	b, lb, sb = mk("bo")
	t.Cleanup(func() { a.Disconnect(sa); b.Disconnect(sb) })
	return
}

func TestRepliesAndReactions(t *testing.T) {
	ch := "#rr" + newID()[:4]
	a, b, la, lb, sa, sb := ergoPair(t, ch)
	b.SendInput(sb, ch, "question from bob")
	m := la.waitFor(t, "bob's message", func(e Event) bool { return e.Type == "msg" && e.Text == "question from bob" })
	msgid, _ := m.Extra["msgid"].(string)
	if msgid == "" {
		t.Fatal("no msgid")
	}
	a.SendReply(sa, ch, msgid, "the answer")
	r := lb.waitFor(t, "reply", func(e Event) bool { return e.Type == "msg" && e.Text == "the answer" })
	if r.Extra["reply"] != msgid {
		t.Fatalf("reply tag missing: %+v", r.Extra)
	}
	own := la.waitFor(t, "own reply echo", func(e Event) bool { return e.Type == "msg" && e.Text == "the answer" })
	if own.Extra["reply"] != msgid || !own.Self {
		t.Fatalf("own echo: %+v", own)
	}
	if err := a.React(sa, ch, msgid, "👍", false); err != "" {
		t.Fatal(err)
	}
	rx := lb.waitFor(t, "reaction", func(e Event) bool { return e.Type == "react" })
	if rx.Text != "👍" || rx.Extra["msgid"] != msgid || rx.Buffer != ch || rx.Extra["remove"] != false {
		t.Fatalf("%+v", rx)
	}
	la.waitFor(t, "own reaction echo", func(e Event) bool { return e.Type == "react" && e.Self })
	a.React(sa, ch, msgid, "👍", true)
	if rm := lb.waitFor(t, "unreact", func(e Event) bool { return e.Type == "react" }); rm.Extra["remove"] != true {
		t.Fatalf("%+v", rm)
	}
	// reactions are logged with the channel, so they come back with history
	found := false
	for _, e := range b.GetHistory(sb, ch) {
		if e.Type == "react" && e.Text == "👍" {
			found = true
		}
	}
	if !found {
		t.Fatal("reaction not in history")
	}
	// commands still work in reply mode
	a.SendReply(sa, ch, msgid, "/me nods")
	lb.waitFor(t, "action", func(e Event) bool { return e.Type == "msg" && e.Kind == "action" && e.Text == "nods" })
}

func waitDCC(t *testing.T, server, dir, state string) DCCTransfer {
	t.Helper()
	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		for _, x := range (&App{}).GetDCCTransfers() {
			if x.Server == server && x.Dir == dir && x.State == state {
				return x
			}
			if x.Server == server && x.Dir == dir && x.State == "failed" {
				t.Fatalf("dcc failed: %s", x.Error)
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for dcc %s %s", dir, state)
	return DCCTransfer{}
}

func TestDCCOverIRC(t *testing.T) {
	ch := "#dc" + newID()[:4]
	a, b, la, lb, sa, sb := ergoPair(t, ch)
	_ = la
	bnick := b.conn(sb).Nick()
	anick := a.conn(sa).Nick()
	data := bytes.Repeat([]byte{1, 2, 3, 250}, 300000)
	src := filepath.Join(t.TempDir(), "photo.jpg")
	os.WriteFile(src, data, 0o644)
	b.store.Update(func(c *Config) { c.Settings.DCCFolder = filepath.Join(t.TempDir(), "recv") })

	if err := a.DCCSend(sa, bnick, src); err != "" {
		t.Fatal(err)
	}
	offer := waitDCC(t, sb, "in", "offer")
	if offer.File != "photo.jpg" || offer.Size != int64(len(data)) || offer.Nick != anick {
		t.Fatalf("%+v", offer)
	}
	if err := b.DCCAccept(offer.ID); err != "" {
		t.Fatal(err)
	}
	done := waitDCC(t, sb, "in", "done")
	waitDCC(t, sa, "out", "done")
	got, _ := os.ReadFile(done.Path)
	if !bytes.Equal(got, data) {
		t.Fatalf("got %d bytes", len(got))
	}

	// direct chat
	if err := b.DCCChat(sb, anick); err != "" {
		t.Fatal(err)
	}
	co := waitDCC(t, sa, "in", "offer")
	if co.Kind != "chat" {
		t.Fatalf("%+v", co)
	}
	a.DCCAccept(co.ID)
	waitDCC(t, sb, "out", "active")
	lb.waitFor(t, "chat open", func(e Event) bool { return e.Type == "buffer" && e.Buffer == "="+anick })
	time.Sleep(200 * time.Millisecond)
	a.SendInput(sa, "="+bnick, "hello directly")
	lb.waitFor(t, "dcc line", func(e Event) bool {
		return e.Type == "msg" && e.Buffer == "="+anick && e.Text == "hello directly" && e.Nick == anick
	})
	b.SendInput(sb, "="+anick, "/me waves")
	la.waitFor(t, "dcc action", func(e Event) bool { return e.Type == "msg" && e.Kind == "action" && e.Text == "waves" })
	a.DCCDecline(co.ID)
	lb.waitFor(t, "closed", func(e Event) bool { return e.Type == "info" && e.Buffer == "="+anick && e.Text == "Direct chat with "+anick+" closed." })
}
