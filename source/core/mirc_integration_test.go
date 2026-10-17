//go:build integration

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mIRC scripts against ergo on 127.0.0.1:6667.
func TestMircScripts(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "mrc")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	id := newID()[:4]
	ch := "#mrc" + id
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	a.store.Update(func(c *Config) {
		c.Servers = nil
		c.Plugins.ScriptsEnabled = true
	})
	dir := a.plugins.pluginDir()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "test.mrc"), []byte(`
; a typical shared script
on *:TEXT:!hi*:#:{
  inc %hi. [ $+ [ $nick ] ]
  if (%hi. [ $+ [ $nick ] ] > 1) return
  msg $chan Hi $nick $+ ! count: %hi. [ $+ [ $nick ] ]
}
on *:TEXT:!calc *:#:msg $chan $2- = $calc($2-)
on !*:JOIN:#:{ notice $nick Welcome to $chan $+ , $nick }
on *:INPUT:#:{
  if ($1 == !shout) { say $upper($2-) | halt }
  if (secret* iswm $1-) halt
}
alias greet {
  if ($1 == $null) { echo -a usage: /greet nick | return }
  msg $chan Hello $1 from $me
}
alias later timer 1 1 msg $chan tick $!nick
on *:NICK:{ if ($newnick == $me) return | msg #mrc`+id+` $nick is now $newnick }
`), 0o644)
	msg := a.plugins.LoadScripts()
	if !strings.Contains(msg, "test.mrc") {
		t.Fatalf("not loaded: %s", msg)
	}
	sv := a.SaveServer(ServerConfig{Name: "Ergo", Host: "127.0.0.1", Port: 6667, Nick: "mrc" + id, AutoJoin: []string{ch}})
	a.Connect(sv.ID)
	log.waitFor(t, "joined", func(e Event) bool { return e.Type == "buffer" && e.Buffer == ch })

	bob := dialRaw(t, "bob"+id)
	bob.send("JOIN " + ch)
	bob.waitFor(t, "Welcome to "+ch+", bob"+id, 4*time.Second)

	bob.send("PRIVMSG " + ch + " :!hi there")
	bob.waitFor(t, "Hi bob"+id+"! count: 1", 4*time.Second)

	bob.send("PRIVMSG " + ch + " :!calc 2 + 3 * 4")
	bob.waitFor(t, "2 + 3 * 4 = 14", 4*time.Second)

	// aliases run as /commands
	a.SendInput(sv.ID, ch, "/greet bob")
	bob.waitFor(t, "Hello bob from mrc"+id, 4*time.Second)

	// on INPUT can rewrite or swallow what you type
	a.SendInput(sv.ID, ch, "!shout make it loud")
	bob.waitFor(t, "MAKE IT LOUD", 4*time.Second)
	a.SendInput(sv.ID, ch, "secret password")
	a.SendInput(sv.ID, ch, "normal line")
	noneBefore(t, bob, "normal line", "secret password", "!shout")

	// timers, with $!nick evaluated when it fires
	a.SendInput(sv.ID, ch, "/later")
	bob.waitFor(t, "tick mrc"+id, 4*time.Second)

	// %hi.<nick> was counted, so a second !hi gets no reply
	bob.send("PRIVMSG " + ch + " :!hi again")
	bob.send("PRIVMSG " + ch + " :!calc 1+1")
	noneBefore(t, bob, "1+1 = 2", "Hi bob")

	bob.send("NICK rob" + id)
	bob.waitFor(t, "bob"+id+" is now rob"+id, 4*time.Second)
	if got := a.PluginCommands(); !strings.Contains(strings.Join(got, ","), "greet") {
		t.Fatalf("aliases missing from completion: %v", got)
	}
}

// noneBefore waits for until, then fails if any of bad arrived before it.
func noneBefore(t *testing.T, r *rawClient, until string, bad ...string) {
	t.Helper()
	end := time.Now().Add(4 * time.Second)
	for time.Now().Before(end) {
		r.mu.Lock()
		for i, l := range r.lines {
			if strings.Contains(l, until) {
				for _, p := range r.lines[:i] {
					for _, b := range bad {
						if strings.Contains(p, b) {
							r.mu.Unlock()
							t.Fatalf("got %q before %q", p, until)
						}
					}
				}
				r.lines = r.lines[i+1:]
				r.mu.Unlock()
				return
			}
		}
		r.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %q", until)
}
