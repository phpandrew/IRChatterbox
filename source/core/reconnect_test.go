package core

import (
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// Pressing Connect while a connection waits to retry must retry now, not start a second
// connection loop next to the first (two loops sharing one connection broke SASL logins).
func TestConnectDuringReconnectWait(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepts atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepts.Add(1)
			c.Close() // drop it straight away → client waits, then retries
		}
	}()
	tmp, _ := os.MkdirTemp("", "rc")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	a := NewApp()
	a.store.Update(func(c *Config) { c.Servers = nil })
	sv := a.SaveServer(ServerConfig{Name: "rc", Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Nick: "rc"})
	a.Connect(sv.ID)
	time.Sleep(700 * time.Millisecond) // first attempt dropped; now waiting 5s to retry
	a.Connect(sv.ID)                   // user presses Connect
	time.Sleep(700 * time.Millisecond)
	if n := accepts.Load(); n != 2 {
		t.Fatalf("after pressing Connect: %d connections, want 2 (retry now)", n)
	}
	time.Sleep(5 * time.Second) // a stray second loop would reconnect here
	if n := accepts.Load(); n != 2 {
		t.Fatalf("%d connections: a second connection loop is running", n)
	}
	a.Disconnect(sv.ID)
	time.Sleep(300 * time.Millisecond)
}
