//go:build integration

package core

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	lb.waitFor(t, "closed", func(e Event) bool {
		return e.Type == "info" && e.Buffer == "="+anick && e.Text == "Direct chat with "+anick+" closed."
	})
}

func TestAwayListAndProxyOnErgo(t *testing.T) {
	ch := "#aw" + newID()[:4]
	a, b, la, _, sa, sb := ergoPair(t, ch)
	bnick := b.conn(sb).Nick()
	// bob is away before a third client joins: that client must see him as away straight away
	b.SendInput(sb, ch, "/away gone fishing")
	time.Sleep(300 * time.Millisecond)

	// the third client connects through a SOCKS5 proxy
	paddr, asked := fakeSOCKS5(t, "", "")
	ph, pp, _ := strings.Cut(paddr, ":")
	port, _ := strconv.Atoi(pp)
	c3 := newBackupApp(t)
	l3 := &evLog{}
	c3.testHook = l3.add
	c3.store.Update(func(c *Config) { c.Servers = nil })
	sv := c3.SaveServer(ServerConfig{Name: "Ergo", Host: "127.0.0.1", Port: 6667, Nick: "px" + newID()[:4], AutoJoin: []string{ch},
		ProxyType: "socks5", ProxyHost: ph, ProxyPort: port})
	c3.Connect(sv.ID)
	defer c3.Disconnect(sv.ID)
	if got := <-asked; got != "127.0.0.1:6667" {
		t.Fatalf("proxy asked for %s", got)
	}
	l3.waitFor(t, "users with bob away", func(e Event) bool {
		if e.Type != "users" || e.Buffer != ch {
			return false
		}
		for _, u := range e.Extra["users"].([]User) {
			if u.Nick == bnick && u.Away {
				return true
			}
		}
		return false
	})
	for _, e := range l3.all {
		if e.Type == "info" && strings.Contains(e.Text, "End of WHO") {
			t.Fatal("the automatic WHO was shown")
		}
	}
	// coming back is seen live (away-notify)
	b.SendInput(sb, ch, "/away")
	la.waitFor(t, "bob back", func(e Event) bool {
		if e.Type != "users" || e.Buffer != ch {
			return false
		}
		for _, u := range e.Extra["users"].([]User) {
			if u.Nick == bnick && !u.Away {
				return true
			}
		}
		return false
	})
	// the channel list is collected, not printed
	if err := a.ListChannels(sa, ""); err != "" {
		t.Fatal(err)
	}
	time.Sleep(800 * time.Millisecond)
	c := a.conn(sa)
	c.mu.Lock()
	active := c.listActive
	c.mu.Unlock()
	if active {
		t.Fatal("list never finished")
	}
	for _, e := range la.all {
		if e.Type == "info" && e.Kind == "list" {
			t.Fatal("list lines printed while browsing")
		}
	}
}

// "/wait" then "/msg bot !invite" in Perform; the invite to an auto-join channel is joined.
func TestPerformInviteJoin(t *testing.T) {
	id := newID()[:4]
	ch := "#inv" + id
	op := newBackupApp(t)
	lo := &evLog{}
	op.testHook = lo.add
	op.store.Update(func(c *Config) { c.Servers = nil })
	so := op.SaveServer(ServerConfig{Name: "Ergo", Host: "127.0.0.1", Port: 6667, Nick: "bot" + id, AutoJoin: []string{ch}})
	op.Connect(so.ID)
	defer op.Disconnect(so.ID)
	lo.waitFor(t, "bot joined", func(e Event) bool { return e.Type == "buffer" && e.Buffer == ch })
	op.SendInput(so.ID, ch, "/mode "+ch+" +i")
	time.Sleep(300 * time.Millisecond)

	me := newBackupApp(t)
	lm := &evLog{}
	me.testHook = lm.add
	me.store.Update(func(c *Config) { c.Servers = nil })
	sm := me.SaveServer(ServerConfig{Name: "Ergo", Host: "127.0.0.1", Port: 6667, Nick: "me" + id, AutoJoin: []string{ch},
		Perform: []string{"/wait 0.5", "/msg bot" + id + " !invite please"}})
	start := time.Now()
	me.Connect(sm.ID)
	defer me.Disconnect(sm.ID)
	req := lo.waitFor(t, "invite request", func(e Event) bool { return e.Type == "msg" && e.Text == "!invite please" })
	if time.Since(start) < 500*time.Millisecond {
		t.Fatal("/wait didn't wait")
	}
	op.SendInput(so.ID, ch, "/invite "+req.Nick+" "+ch)
	lm.waitFor(t, "joined after invite", func(e Event) bool { return e.Type == "buffer" && e.Buffer == ch })
}

// A pasted block goes out as one draft/multiline message and arrives as one, line breaks kept.
func TestMultilineOnErgo(t *testing.T) {
	ch := "#ml" + newID()[:4]
	a, b, la, lb, sa, sb := ergoPair(t, ch)
	_ = b
	_ = sb
	if !a.CanSendMultiline(sa, 3) {
		t.Fatal("ergo should offer draft/multiline")
	}
	long := strings.Repeat("word ", 120) // longer than one IRC line: sent in pieces, joined back
	a.SendInput(sa, ch, "first line\n\n  indented\n"+long)
	want := "first line\n\n  indented\n" + strings.TrimRight(long, " ")
	got := lb.waitFor(t, "multiline", func(e Event) bool { return e.Type == "msg" && strings.HasPrefix(e.Text, "first line") })
	if strings.TrimRight(got.Text, " ") != want {
		t.Fatalf("got %q", got.Text)
	}
	own := la.waitFor(t, "own echo", func(e Event) bool { return e.Type == "msg" && strings.HasPrefix(e.Text, "first line") })
	if !own.Self {
		t.Fatal("echo not marked as mine")
	}
	time.Sleep(300 * time.Millisecond)
	n := 0
	for _, e := range la.all {
		if e.Type == "msg" && strings.HasPrefix(e.Text, "first line") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("own multi-line message shown %d times", n)
	}
	// commands still run one by one
	a.SendInput(sa, ch, "/me waves\nhello after")
	lb.waitFor(t, "action", func(e Event) bool { return e.Kind == "action" && e.Text == "waves" })
	lb.waitFor(t, "plain", func(e Event) bool { return e.Type == "msg" && e.Text == "hello after" })
}

// A generated client certificate is presented over TLS, added to the account with
// NickServ CERT ADD, and then logs in on its own with SASL EXTERNAL.
func TestClientCertSASLExternal(t *testing.T) {
	id := newID()[:4]
	a := newBackupApp(t)
	l := &evLog{}
	a.testHook = l.add
	a.store.Update(func(c *Config) { c.Servers = nil })
	cc := a.GenerateClientCert("cf" + id)
	if cc.Error != "" || len(cc.Fingerprint) != 64 {
		t.Fatalf("%+v", cc)
	}
	sv := a.SaveServer(ServerConfig{Name: "Ergo TLS", Host: "127.0.0.1", Port: 6697, TLS: true, TLSSkipVerify: true, Nick: "cf" + id,
		ClientCert: cc.Cert, ClientKey: cc.Key})
	a.Connect(sv.ID)
	l.waitFor(t, "connected", func(e Event) bool { return e.Type == "status" && e.Extra["status"] == "connected" })
	a.SendInput(sv.ID, "", "/msg NickServ REGISTER hunter2-"+id)
	l.waitFor(t, "registered", func(e Event) bool { return strings.Contains(strings.ToLower(e.Text), "registered") })
	a.SendInput(sv.ID, "", "/msg NickServ CERT ADD")
	l.waitFor(t, "cert added", func(e Event) bool {
		return strings.Contains(strings.ToLower(e.Text), "fingerprint") || strings.Contains(strings.ToLower(e.Text), "certificate")
	})
	a.Disconnect(sv.ID)
	time.Sleep(500 * time.Millisecond)

	got, _ := a.store.Server(sv.ID)
	got.AuthMethod = "external"
	a.SaveServer(got)
	a.Connect(sv.ID)
	defer a.Disconnect(sv.ID)
	l.waitFor(t, "SASL EXTERNAL", func(e Event) bool { return e.Type == "info" && e.Text == "SASL authentication successful" })
}
