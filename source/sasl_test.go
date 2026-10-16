//go:build integration
// Run against a local IRC server on 127.0.0.1:6667/6697 (e.g. ergo): go test -tags integration .

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestSASLAndNickServ(t *testing.T) {
	tmp := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	acct := "u" + newID()[:6]
	r := dialRaw(t, acct)
	r.send("PRIVMSG NickServ :REGISTER hunter2pass")
	r.waitFor(t, "registered", 3*time.Second)
	r.send("QUIT")

	for _, method := range []string{"sasl", "nickserv"} {
		a := NewApp()
		log := &evLog{}
		a.testHook = log.add
		a.store.Update(func(c *Config) { c.Servers = nil })
		sv := a.SaveServer(ServerConfig{Name: "L", Host: "127.0.0.1", Port: 6667, Nick: acct, AuthMethod: method, AuthUser: acct, AuthPass: "hunter2pass"})
		a.Connect(sv.ID)
		e := log.waitFor(t, method+" ok", func(e Event) bool {
			return (e.Type == "info" || e.Type == "msg") && (strings.Contains(e.Text, "SASL authentication successful") || strings.Contains(strings.ToLower(e.Text), "logged in") || strings.Contains(strings.ToLower(e.Text), "identified"))
		})
		t.Log(method, "→", e.Text)
		a.Disconnect(sv.ID)
		time.Sleep(500 * time.Millisecond)
	}
}
