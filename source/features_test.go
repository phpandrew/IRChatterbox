//go:build integration

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPluginEventsAndEncoding(t *testing.T) {
	tmp := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmp)
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	js := `
on("join",  function(e){ if (!e.self) say(e.channel, "welcome " + e.nick); });
on("part",  function(e){ say(e.channel, "bye " + e.nick + " (" + e.reason + ")"); });
on("nick",  function(e){ if (!e.self) say("#ev", e.oldNick + " is now " + e.newNick); });
on("topic", function(e){ reply("topic by " + (e.self ? "me" : e.nick) + ": " + e.topic); });
on("action",function(e){ reply("saw action from " + e.nick + ": " + e.text); });
on("connect", function(e){ log("connected as " + e.nick); });
command("count", function(e){ var n = store.get("n", 0) + 1; store.set("n", n); reply("count " + n + " args=" + e.argv.join(",")); });
command("later", function(e){ setTimeout(function(){ reply("timer fired"); }, 300); });
on("message", function(e){ if (e.text === "!users") reply("users: " + users(e.channel).length + " op=" + isOp(e.channel, me())); });
`
	os.WriteFile(filepath.Join(a.store.dir, "plugins", "ev.js"), []byte(js), 0o644)
	t.Log(a.plugins.LoadScripts())
	sv := a.SaveServer(ServerConfig{Name: "L", Host: "127.0.0.1", Port: 6667, Nick: "Ev" + newID()[:4], AutoJoin: []string{"#ev"}, Encoding: "auto"})
	a.Connect(sv.ID)
	log.waitFor(t, "join", func(e Event) bool { return e.Type == "buffer" && e.Buffer == "#ev" })
	log.waitFor(t, "users", func(e Event) bool { return e.Type == "users" && e.Buffer == "#ev" })

	nick := "bob" + newID()[:3]
	bob := dialRaw(t, nick)
	bob.send("JOIN #ev")
	t.Log(bob.waitFor(t, "welcome "+nick, 4*time.Second))
	a.SendInput(sv.ID, "#ev", "/topic hello topic")
	t.Log(bob.waitFor(t, "topic by me: hello topic", 4*time.Second))
	bob.send("PRIVMSG #ev :\x01ACTION waves\x01")
	t.Log(bob.waitFor(t, "saw action from", 4*time.Second))
	bob.send("PRIVMSG #ev :!users")
	t.Log(bob.waitFor(t, "users: 2", 4*time.Second))

	a.SendInput(sv.ID, "#ev", "/count a b")
	t.Log(bob.waitFor(t, "count 1 args=a,b", 4*time.Second))
	a.SendInput(sv.ID, "#ev", "/count")
	t.Log(bob.waitFor(t, "count 2", 4*time.Second))
	a.SendInput(sv.ID, "#ev", "/later")
	t.Log(bob.waitFor(t, "timer fired", 4*time.Second))
	b, _ := os.ReadFile(filepath.Join(a.store.dir, "plugins", "data", "ev.json"))
	t.Log("store file:", strings.TrimSpace(string(b)))

	// Latin-1 / Windows-1252 bytes from an old client: "café 20°" (é=0xE9, °=0xB0)
	bob.send("PRIVMSG #ev :caf\xe9 \xb0 test")
	e := log.waitFor(t, "latin1", func(e Event) bool { return e.Type == "msg" && strings.Contains(e.Text, "test") && strings.HasPrefix(e.Text, "caf") })
	t.Logf("decoded: %q", e.Text)
	if e.Text != "café ° test" {
		t.Fatalf("bad decode %q", e.Text)
	}

	newNick := nick + "x"
	bob.send("NICK " + newNick)
	t.Log(bob.waitFor(t, nick+" is now "+newNick, 4*time.Second))
	bob.send("PART #ev :gone")
	time.Sleep(500 * time.Millisecond)
	a.Disconnect(sv.ID)
	time.Sleep(300 * time.Millisecond)

	// explicit Cyrillic encoding: outgoing text is encoded as windows-1251
	sv.Encoding = "windows-1251"
	c := newConn(a, sv)
	enc := c.encode("PRIVMSG #x :Привет")
	t.Logf("cp1251 bytes: % x", []byte(enc[len("PRIVMSG #x :"):]))
	if c.decode(enc) != "PRIVMSG #x :Привет" {
		t.Fatal("round trip failed")
	}
}
