package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// A fake ZNC: checks the PASS login and replays a playback buffer, with and without batches.
func TestZNCPlayback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	gotPass := make(chan string, 1)
	gotOut := make(chan string, 50)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewScanner(c)
		w := func(s string) { fmt.Fprintf(c, "%s\r\n", s) }
		old := time.Now().Add(-2 * time.Hour).UTC()
		ts := func(m int) string { return old.Add(time.Duration(m) * time.Minute).Format(isoMilli) }
		for r.Scan() {
			l := r.Text()
			switch {
			case strings.HasPrefix(l, "PASS "):
				gotPass <- strings.TrimPrefix(l, "PASS ")
			case strings.HasPrefix(l, "CAP LS"):
				w(":znc CAP * LS :server-time batch znc.in/playback")
			case strings.HasPrefix(l, "CAP REQ"):
				w(":znc CAP * ACK :" + strings.SplitN(l, ":", 2)[1])
			case l == "CAP END":
				w(":znc 001 andy :Welcome")
				w(":andy!a@h JOIN #old")
				w(":znc BATCH +p znc.in/playback #old")
				w("@batch=p;time=" + ts(1) + " :bob!b@h PRIVMSG #old :earlier 20c chat")
				w("@batch=p;time=" + ts(2) + " :bob!b@h PRIVMSG #old :andy ping from the past")
				w("@batch=p;time=" + ts(3) + " :bob!b@h PRIVMSG #old :\x01VERSION\x01")
				w(":znc BATCH -p")
				// older ZNC without batch: only the timestamp gives it away
				w("@time=" + ts(4) + " :bob!b@h PRIVMSG #old :unbatched old line")
				w("@time=" + time.Now().UTC().Format(isoMilli) + " :bob!b@h PRIVMSG #old :live line")
			default:
				gotOut <- l
			}
		}
	}()
	tmp, _ := os.MkdirTemp("", "znc")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	a.store.Update(func(c *Config) { c.Servers = nil; c.Plugins.WeatherCooldown = 0 })
	port := ln.Addr().(*net.TCPAddr).Port
	sv := a.SaveServer(ServerConfig{Name: "ZNC", Host: "127.0.0.1", Port: port, Nick: "andy",
		Bouncer: "znc", BouncerUser: "andrew", BouncerPass: "pw", BouncerNetwork: "libera"})
	a.Connect(sv.ID)
	defer a.Disconnect(sv.ID)

	select {
	case p := <-gotPass:
		if p != "andrew/libera:pw" {
			t.Fatalf("PASS = %q, want andrew/libera:pw", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no PASS sent")
	}
	for _, want := range []string{"earlier 20c chat", "andy ping from the past", "unbatched old line"} {
		e := log.waitFor(t, want, func(e Event) bool { return e.Type == "msg" && e.Text == want })
		if e.Extra["replay"] != true {
			t.Fatalf("%q should be a replay", want)
		}
	}
	e := log.waitFor(t, "live", func(e Event) bool { return e.Type == "msg" && e.Text == "live line" })
	if e.Extra["replay"] == true {
		t.Fatal("live line marked as replay")
	}
	time.Sleep(500 * time.Millisecond)
	for len(gotOut) > 0 {
		l := <-gotOut
		if strings.Contains(l, "[Bot]") || strings.Contains(l, "VERSION IRChatterbox") {
			t.Fatalf("replay triggered a reply: %s", l)
		}
	}
}
