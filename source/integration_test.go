//go:build integration
// Run against a local IRC server on 127.0.0.1:6667/6697 (e.g. ergo): go test -tags integration .

package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type rawClient struct {
	c     net.Conn
	mu    sync.Mutex
	lines []string
}

func dialRaw(t *testing.T, nick string) *rawClient {
	c, err := net.Dial("tcp", "127.0.0.1:6667")
	if err != nil {
		t.Fatal(err)
	}
	r := &rawClient{c: c}
	go func() {
		sc := bufio.NewScanner(c)
		for sc.Scan() {
			l := sc.Text()
			if strings.HasPrefix(l, "PING") {
				fmt.Fprintf(c, "PONG %s\r\n", l[5:])
			}
			r.mu.Lock()
			r.lines = append(r.lines, l)
			r.mu.Unlock()
		}
	}()
	r.send("NICK " + nick)
	r.send("USER " + nick + " 0 * :" + nick)
	r.waitFor(t, " 001 ", 5*time.Second)
	return r
}
func (r *rawClient) send(l string) { fmt.Fprintf(r.c, "%s\r\n", l) }
func (r *rawClient) waitFor(t *testing.T, sub string, d time.Duration) string {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		r.mu.Lock()
		for i, l := range r.lines {
			if strings.Contains(l, sub) {
				r.lines = r.lines[i+1:]
				r.mu.Unlock()
				return l
			}
		}
		r.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %q", sub)
	return ""
}

type evLog struct {
	mu  sync.Mutex
	evs []Event
}

func (l *evLog) add(e Event) { l.mu.Lock(); l.evs = append(l.evs, e); l.mu.Unlock() }
func (l *evLog) waitFor(t *testing.T, desc string, f func(Event) bool) Event {
	end := time.Now().Add(6 * time.Second)
	for time.Now().Before(end) {
		l.mu.Lock()
		for i, e := range l.evs {
			if f(e) {
				l.evs = l.evs[i+1:]
				l.mu.Unlock()
				return e
			}
		}
		l.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for event: %s", desc)
	return Event{}
}

func TestIntegration(t *testing.T) {
	tmp := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)

	// fake solar endpoint
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"power": 4210, "today_kwh": 18.734, "status":"ok"}`))
	}))
	defer hs.Close()

	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	os.WriteFile(filepath.Join(a.store.dir, "plugins", "roll.js"), []byte(`on("message", function(e){ if (e.text === "!roll") reply(prefix + "rolled for " + e.nick); });`), 0o644)
	t.Log(a.plugins.LoadScripts())

	a.store.Update(func(c *Config) {
		c.Servers = nil
		c.Plugins.URLCommands = []URLCommand{{Trigger: "!solar", URL: hs.URL, Template: "Solar: {{.power}} W, today {{round .today_kwh 1}} kWh", MaxLines: 3, Cooldown: 0, Enabled: true}}
		c.Plugins.WeatherCooldown = 0
		c.Settings.HighlightWords = []string{"tshirtgang"}
	})
	sv := a.SaveServer(ServerConfig{Name: "Local", Host: "127.0.0.1", Port: 6697, TLS: true, TLSSkipVerify: true, Nick: "Andrew", AuthMethod: "none", AutoJoin: []string{"#test"}, RememberChans: true})
	a.Connect(sv.ID)
	log.waitFor(t, "connected", func(e Event) bool { return e.Type == "status" && e.Extra["status"] == "connected" })
	log.waitFor(t, "joined #test", func(e Event) bool { return e.Type == "buffer" && e.Buffer == "#test" })
	log.waitFor(t, "users", func(e Event) bool { return e.Type == "users" && e.Buffer == "#test" })

	bob := dialRaw(t, "bob")
	bob.send("JOIN #test")
	bob.waitFor(t, "366", 3*time.Second)
	log.waitFor(t, "bob join", func(e Event) bool { return e.Type == "event" && e.Kind == "join" && e.Nick == "bob" })

	// weather from others
	bob.send("PRIVMSG #test :Today it feels like 20c")
	l := bob.waitFor(t, "[Bot]", 4*time.Second)
	t.Log("bob saw:", l)
	if !strings.Contains(l, "[Bot] 20 C = 68 F") {
		t.Fatalf("bad weather reply %q", l)
	}
	// weather from me
	a.SendInput(sv.ID, "#test", "it's 72F in here")
	l = bob.waitFor(t, "[Bot]", 4*time.Second)
	t.Log("bob saw:", l)

	// highlight
	bob.send("PRIVMSG #test :hey Andrew check this")
	e := log.waitFor(t, "hl", func(e Event) bool { return e.Type == "msg" && e.Nick == "bob" && strings.Contains(e.Text, "hey") })
	if !e.Highlight {
		t.Fatal("expected highlight")
	}
	bob.send("PRIVMSG #test :the tshirtgang server")
	e = log.waitFor(t, "hl word", func(e Event) bool { return e.Type == "msg" && strings.Contains(e.Text, "tshirtgang") })
	if !e.Highlight {
		t.Fatal("expected highlight word")
	}
	bob.send("PRIVMSG #test :Andrews is not me")
	e = log.waitFor(t, "no hl", func(e Event) bool { return e.Type == "msg" && strings.Contains(e.Text, "Andrews") })
	if e.Highlight {
		t.Fatal("false highlight")
	}

	// url command
	bob.send("PRIVMSG #test :!solar")
	l = bob.waitFor(t, "Solar:", 5*time.Second)
	t.Log("bob saw:", l)
	// js plugin
	bob.send("PRIVMSG #test :!roll")
	l = bob.waitFor(t, "rolled for bob", 5*time.Second)

	// ops commands
	a.SendInput(sv.ID, "#test", "/op bob")
	log.waitFor(t, "mode", func(e Event) bool { return e.Type == "event" && e.Kind == "mode" && strings.Contains(e.Text, "+o bob") })
	e = log.waitFor(t, "users w/ op", func(e Event) bool {
		if e.Type != "users" {
			return false
		}
		for _, u := range e.Extra["users"].([]User) {
			if u.Nick == "bob" && u.Prefix == "@" {
				return true
			}
		}
		return false
	})
	a.SendInput(sv.ID, "#test", "/topic Hello world")
	log.waitFor(t, "topic", func(e Event) bool { return e.Type == "topic" && e.Text == "Hello world" })
	a.SendInput(sv.ID, "#test", "/me waves")
	bob.waitFor(t, "ACTION waves", 3*time.Second)

	// PM + query + notice routing
	bob.send("PRIVMSG Andrew :private hi")
	e = log.waitFor(t, "pm", func(e Event) bool { return e.Type == "msg" && e.Buffer == "bob" })
	bob.send("NOTICE Andrew :a notice")
	e = log.waitFor(t, "notice", func(e Event) bool { return e.Type == "msg" && e.Kind == "notice" })
	t.Log("notice buffer:", e.Buffer)

	// nick change
	bob.send("NICK robert")
	log.waitFor(t, "nick", func(e Event) bool { return e.Type == "event" && e.Kind == "nick" && e.Text == "robert" })
	a.SendInput(sv.ID, "#test", "/kick robert bye")
	log.waitFor(t, "kick", func(e Event) bool { return e.Type == "event" && e.Kind == "kick" })

	// raw pass-through + whois
	a.SendInput(sv.ID, "", "/whois Andrew")
	log.waitFor(t, "whois", func(e Event) bool { return e.Type == "info" && e.Kind == "whois" })
	a.SendInput(sv.ID, "", "/lusers")
	log.waitFor(t, "lusers", func(e Event) bool { return e.Type == "info" && strings.Contains(e.Text, "users") })

	// long message split
	long := strings.Repeat("word ", 200)
	a.SendInput(sv.ID, "#test", "/join #two")
	log.waitFor(t, "#two", func(e Event) bool { return e.Type == "buffer" && e.Buffer == "#two" })
	a.SendInput(sv.ID, "#two", long)

	// nick colour + remember channels
	a.SendInput(sv.ID, "#test", "/color bob #ff8800")
	if a.store.Get().NickColors["bob"] != "#ff8800" {
		t.Fatal("colour not saved")
	}
	cfg, _ := a.store.Server(sv.ID)
	t.Log("autojoin:", cfg.AutoJoin)
	a.SendInput(sv.ID, "#two", "/part")
	time.Sleep(300 * time.Millisecond)
	cfg, _ = a.store.Server(sv.ID)
	t.Log("autojoin after part:", cfg.AutoJoin)

	// history
	h := a.GetHistory(sv.ID, "#test")
	t.Logf("history lines: %d, last: %+v", len(h), h[len(h)-1].Text)

	// reconnect on server drop handled? disconnect cleanly
	a.Disconnect(sv.ID)
	log.waitFor(t, "disconnected", func(e Event) bool { return e.Type == "status" && e.Extra["status"] == "disconnected" })
}
