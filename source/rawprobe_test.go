//go:build probe

package main

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// go test -tags probe -run TestProbe  with PROBE_HOST, PROBE_USER, PROBE_PASS, PROBE_NET set:
// connects like the app does and prints the raw IRC conversation.
func TestProbe(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "probe")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	debugRaw = func(l string) { fmt.Println(time.Now().Format("15:04:05.000"), l) }
	a := NewApp()
	a.store.Update(func(c *Config) { c.Servers = nil })
	sv := a.SaveServer(ServerConfig{Name: "probe", Host: os.Getenv("PROBE_HOST"), Port: 6697, TLS: true, Nick: "probe" + newID()[:3],
		Bouncer: "soju", BouncerUser: os.Getenv("PROBE_USER"), BouncerPass: os.Getenv("PROBE_PASS"), BouncerNetwork: os.Getenv("PROBE_NET")})
	a.Connect(sv.ID)
	time.Sleep(6 * time.Second)
	a.Disconnect(sv.ID)
	time.Sleep(time.Second)
}
