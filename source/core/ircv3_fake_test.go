package core

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func selfSignedTLS(t *testing.T) *tls.Config {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"127.0.0.1", "localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

// fakeIRC answers registration and runs script(w) once the client sends CAP END.
func fakeIRC(t *testing.T, ln net.Listener, caps string, got chan<- string, script func(w func(string))) {
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				w := func(s string) { fmt.Fprintf(c, "%s\r\n", s) }
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					l := sc.Text()
					select {
					case got <- l:
					default:
					}
					switch {
					case strings.HasPrefix(l, "CAP LS"):
						w(":srv CAP * LS :" + caps)
					case strings.HasPrefix(l, "CAP REQ"):
						w(":srv CAP * ACK :" + strings.SplitN(l, ":", 2)[1])
					case l == "CAP END":
						w(":srv 001 me :Welcome")
						w(":me!u@h JOIN #c")
						if script != nil {
							script(w)
						}
					}
				}
			}(c)
		}
	}()
}

// A plain-text server advertising sts=port=N makes the client switch to TLS on that port
// and keep it that way.
func TestSTSUpgrade(t *testing.T) {
	plain, _ := net.Listen("tcp", "127.0.0.1:0")
	defer plain.Close()
	secure, _ := tls.Listen("tcp", "127.0.0.1:0", selfSignedTLS(t))
	defer secure.Close()
	tlsPort := secure.Addr().(*net.TCPAddr).Port
	gotPlain, gotTLS := make(chan string, 100), make(chan string, 100)
	fakeIRC(t, plain, fmt.Sprintf("sts=port=%d,duration=300 server-time", tlsPort), gotPlain, nil)
	fakeIRC(t, secure, "sts=duration=300 server-time", gotTLS, nil)

	tmp, _ := os.MkdirTemp("", "sts")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	a.store.Update(func(c *Config) { c.Servers = nil })
	sv := a.SaveServer(ServerConfig{Name: "sts", Host: "127.0.0.1", Port: plain.Addr().(*net.TCPAddr).Port, Nick: "me", TLSSkipVerify: true})
	a.Connect(sv.ID)
	defer a.Disconnect(sv.ID)
	log.waitFor(t, "sts notice", func(e Event) bool { return e.Type == "info" && strings.Contains(e.Text, "STS") })
	select {
	case <-gotTLS:
	case <-time.After(5 * time.Second):
		t.Fatal("never reconnected over TLS")
	}
	log.waitFor(t, "connected over TLS", func(e Event) bool { return e.Type == "status" && e.Extra["status"] == "connected" })
	saved, _ := a.store.Server(sv.ID)
	if !saved.TLS || saved.Port != tlsPort {
		t.Fatalf("server settings not upgraded: tls=%v port=%d", saved.TLS, saved.Port)
	}
}

// draft/message-redaction: a REDACT from the server hides that message; Redact sends one.
func TestRedaction(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	got := make(chan string, 100)
	fakeIRC(t, ln, "draft/message-redaction message-tags server-time echo-message", got, func(w func(string)) {
		w("@msgid=m1 :bob!b@h PRIVMSG #c :oops wrong channel")
		w(":bob!b@h REDACT #c m1 :posted by mistake")
	})
	tmp, _ := os.MkdirTemp("", "red")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	a.store.Update(func(c *Config) { c.Servers = nil })
	sv := a.SaveServer(ServerConfig{Name: "red", Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Nick: "me"})
	a.Connect(sv.ID)
	defer a.Disconnect(sv.ID)
	m := log.waitFor(t, "message", func(e Event) bool { return e.Type == "msg" && e.Text == "oops wrong channel" })
	if m.Extra["msgid"] != "m1" {
		t.Fatalf("msgid missing: %+v", m.Extra)
	}
	r := log.waitFor(t, "redact", func(e Event) bool { return e.Type == "redact" })
	if r.Text != "m1" || r.Buffer != "#c" || r.Extra["reason"] != "posted by mistake" {
		t.Fatalf("redact event %+v", r)
	}
	if err := a.Redact(sv.ID, "#c", "m2"); err != "" {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case l := <-got:
			if l == "REDACT #c m2" {
				return
			}
		case <-deadline:
			t.Fatal("REDACT not sent")
		}
	}
}
