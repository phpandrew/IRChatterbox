package core

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplyTagAndReactionChecks(t *testing.T) {
	m := ParseMessage(`@msgid=x1;+draft/reply=abc123 :bob!b@h PRIVMSG #c :hi`)
	if got := msgExtra(m)["reply"]; got != "abc123" {
		t.Fatalf("reply = %v", got)
	}
	if replyTag(ParseMessage(`@+draft/reply=bad\sid :b PRIVMSG #c :x`)) != "" {
		t.Fatal("msgid with a space accepted")
	}
	for _, ok := range []string{"👍", "❤️", "+1", "🏳️‍🌈"} {
		if !validReaction(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "a b", strings.Repeat("x", 40), "x\n"} {
		if validReaction(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if tagEscape.Replace(`a;b c\`) != `a\:b\sc\\` {
		t.Fatal("tag escaping")
	}
}

func TestLinkPreviewParse(t *testing.T) {
	page := `<!doctype html><html><head><title>  Fallback &amp; title </title>
<meta property="og:title" content="The Real Title">
<meta name="description" content="Plain description">
<meta property="og:description" content="  OG   description ">
<meta property="og:image" content="/img/card.png">
<meta property="og:site_name" content="Example">
</head><body><meta property="og:title" content="ignored"></body></html>`
	base, _ := url.Parse("https://example.com/post/1")
	var p LinkPreview
	parsePreview(strings.NewReader(page), base, &p)
	if p.Title != "The Real Title" || p.Description != "OG description" || p.Image != "https://example.com/img/card.png" || p.Site != "Example" {
		t.Fatalf("%+v", p)
	}
	p = LinkPreview{}
	parsePreview(strings.NewReader(`<html><head><title>Only &amp; title</title><meta property="og:image" content="javascript:alert(1)"></head>`), base, &p)
	if p.Title != "Only & title" || p.Image != "" || p.Site != "example.com" {
		t.Fatalf("%+v", p)
	}
}

func TestLinkPreviewRefusesLocalAddresses(t *testing.T) {
	for _, a := range []string{"127.0.0.1:80", "10.0.0.5:80", "192.168.1.1:443", "[::1]:80", "169.254.169.254:80", "100.64.1.1:80", "0.0.0.0:80"} {
		if publicOnly("tcp", a, nil) == nil {
			t.Errorf("%s allowed", a)
		}
	}
	if publicOnly("tcp", "93.184.216.34:443", nil) != nil {
		t.Error("public address refused")
	}
	p := fetchPreview(t.Context(), "http://127.0.0.1:1/")
	if p.Error != "local address" {
		t.Fatalf("loopback fetch: %+v", p)
	}
	if fetchPreview(t.Context(), "file:///etc/passwd").Error == "" {
		t.Fatal("file URL accepted")
	}
}

func TestChannelNotifyRules(t *testing.T) {
	a := newBackupApp(t)
	c := &Conn{app: a, cfg: ServerConfig{ID: "s1"}}
	if !a.wantsNotify(c, "bob", "privmsg", true, false) || a.wantsNotify(c, "#c", "privmsg", false, false) || !a.wantsNotify(c, "#c", "privmsg", false, true) {
		t.Fatal("defaults")
	}
	a.SetChannelNotify("s1", "#C", "all", 0)
	if !a.wantsNotify(c, "#c", "privmsg", false, false) || a.wantsNotify(c, "#c", "notice", false, false) {
		t.Fatal("all")
	}
	a.SetChannelNotify("s1", "#c", "mute", 60)
	if a.wantsNotify(c, "#c", "privmsg", false, true) {
		t.Fatal("muted channel notified a mention")
	}
	a.store.Update(func(cf *Config) {
		r := cf.ChannelNotify["s1/#c"]
		r.Until = time.Now().Add(-time.Minute).UnixMilli()
		cf.ChannelNotify["s1/#c"] = r
	})
	if !a.wantsNotify(c, "#c", "privmsg", false, true) || a.notifyLevel("s1", "#c", false) != "mentions" {
		t.Fatal("expired mute still in force")
	}
	a.pruneNotifyRules()
	if _, ok := a.store.Get().ChannelNotify["s1/#c"]; ok {
		t.Fatal("expired mute not pruned")
	}
	a.SetChannelNotify("s1", "bob", "mute", 0)
	if a.wantsNotify(c, "bob", "privmsg", true, false) {
		t.Fatal("muted PM")
	}
	a.SetChannelNotify("s1", "bob", "", 0)
	if !a.wantsNotify(c, "bob", "privmsg", true, false) {
		t.Fatal("reset")
	}
}

func TestDCCParsing(t *testing.T) {
	got := parseDCCArgs(`SEND "my file.txt" 2130706433 5000 1234 tok`)
	if strings.Join(got, "|") != "SEND|my file.txt|2130706433|5000|1234|tok" {
		t.Fatalf("%q", got)
	}
	if ip := parseDCCIP("2130706433"); !ip.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatal(ip)
	}
	if ip := parseDCCIP("::1"); !ip.Equal(net.ParseIP("::1")) {
		t.Fatal(ip)
	}
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd", `C:\Windows\evil.exe`: "evil.exe", "a<b>c?.txt": "a_b_c_.txt",
		"CON.txt": "_CON.txt", "..": "file", "  .hidden. ": "hidden", "x\x00y": "x_y",
	} {
		if got := cleanFileName(in); got != want {
			t.Errorf("cleanFileName(%q) = %q, want %q", in, got, want)
		}
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644)
	if p := freeName(dir, "a.txt"); filepath.Base(p) != "a (1).txt" {
		t.Fatal(p)
	}
}

// A file goes from DCCSend to an accepted offer over loopback, both sides in one test.
func TestDCCTransferLoopback(t *testing.T) {
	a := newBackupApp(t)
	a.store.Update(func(c *Config) {
		c.Settings.DCCAddress = "127.0.0.1"
		c.Settings.DCCFolder = filepath.Join(a.store.dir, "in")
	})
	data := bytes.Repeat([]byte("IRChatterbox DCC "), 50000) // ~850 KB
	src := filepath.Join(t.TempDir(), "src file.bin")
	os.WriteFile(src, data, 0o644)

	// the sending side: listen like DCCSend does, then upload
	l, err := a.dccListen()
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	out := &dccItem{c: &Conn{app: a}}
	out.ID, out.Kind, out.Dir, out.Path, out.Size, out.File = newDCCID(), "send", "out", src, int64(len(data)), "src file.bin"
	go func() {
		conn, err := l.Accept()
		l.Close()
		if err == nil {
			a.dccUpload(t.Context(), out, conn)
			conn.Close()
		}
	}()

	// the receiving side, as onDCC + DCCAccept would set it up
	in := &dccItem{c: &Conn{app: a}}
	in.ID, in.Kind, in.Dir, in.File, in.Size, in.ip, in.port, in.State = newDCCID(), "send", "in", "src file.bin", int64(len(data)), net.ParseIP("127.0.0.1"), port, "offer"
	dccMu.Lock()
	dccItems[in.ID], dccItems[out.ID] = in, out
	dccMu.Unlock()
	if e := a.DCCAccept(in.ID); e != "" {
		t.Fatal(e)
	}
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		dccMu.Lock()
		st, ost := in.State, out.State
		dccMu.Unlock()
		if st == "done" && ost == "done" {
			break
		}
		if st == "failed" {
			t.Fatal(in.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, err := os.ReadFile(in.Path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("received %d bytes (%v), state %s/%s", len(got), err, in.State, out.State)
	}
	if filepath.Base(in.Path) != "src file.bin" {
		t.Fatal(in.Path)
	}
}

// A sender that sends more than it offered: the extra is dropped.
func TestDCCReceiveCapsSize(t *testing.T) {
	a := newBackupApp(t)
	a.store.Update(func(c *Config) { c.Settings.DCCFolder = filepath.Join(a.store.dir, "in2") })
	s1, s2 := net.Pipe()
	go func() {
		s1.Write([]byte("0123456789EXTRA"))
		var b [4]byte
		io.ReadFull(s1, b[:])
		_ = binary.BigEndian.Uint32(b[:])
		s1.Close()
	}()
	it := &dccItem{c: &Conn{app: a}}
	it.ID, it.Kind, it.File, it.Size = newDCCID(), "send", "x.txt", 10
	a.dccDownload(t.Context(), it, s2)
	b, _ := os.ReadFile(it.Path)
	if string(b) != "0123456789" || it.State != "done" {
		t.Fatalf("%q %s %s", b, it.State, it.Error)
	}
}

func TestFavourites(t *testing.T) {
	a := newBackupApp(t)
	a.SetFavourite("s1", "#Go", true)
	a.SetFavourite("s2", "bob", true)
	a.SetFavourite("s1", "#go", true) // again: moves to the end, no duplicate
	f := a.store.Get().Favourites
	if len(f) != 2 || f[0].Buffer != "bob" || f[1].Buffer != "#go" {
		t.Fatalf("%+v", f)
	}
	a.SetFavouriteOrder([]Favourite{{"s1", "#GO"}, {"x", "nope"}})
	if f = a.store.Get().Favourites; f[0].Buffer != "#go" || f[1].Buffer != "bob" || len(f) != 2 {
		t.Fatalf("order %+v", f)
	}
	a.SetFavourite("s1", "#go", false)
	if f = a.store.Get().Favourites; len(f) != 1 || f[0].Server != "s2" {
		t.Fatalf("remove %+v", f)
	}
	a.SetFavourite("s1", "*active*", true)
	if len(a.store.Get().Favourites) != 1 {
		t.Fatal("special buffer accepted")
	}
}
