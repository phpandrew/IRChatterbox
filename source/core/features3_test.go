package core

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeSOCKS5 is a minimal SOCKS5 proxy (optional user/pass) that records where it was asked to go.
func fakeSOCKS5(t *testing.T, user, pass string) (addr string, asked chan string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	asked = make(chan string, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				b := make([]byte, 262)
				io.ReadFull(c, b[:2])
				io.ReadFull(c, b[:b[1]])
				if user != "" {
					c.Write([]byte{5, 2})
					io.ReadFull(c, b[:2])
					u := make([]byte, b[1])
					io.ReadFull(c, u)
					io.ReadFull(c, b[:1])
					p := make([]byte, b[0])
					io.ReadFull(c, p)
					if string(u) != user || string(p) != pass {
						c.Write([]byte{1, 1})
						return
					}
					c.Write([]byte{1, 0})
				} else {
					c.Write([]byte{5, 0})
				}
				io.ReadFull(c, b[:4])
				var host string
				switch b[3] {
				case 1:
					io.ReadFull(c, b[:4])
					host = net.IP(b[:4]).String()
				case 3:
					io.ReadFull(c, b[:1])
					h := make([]byte, b[0])
					io.ReadFull(c, h)
					host = string(h)
				}
				io.ReadFull(c, b[:2])
				port := binary.BigEndian.Uint16(b[:2])
				dest := net.JoinHostPort(host, strconv.Itoa(int(port)))
				asked <- dest
				up, err := net.Dial("tcp", dest)
				if err != nil {
					c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer up.Close()
				c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				go io.Copy(up, c)
				io.Copy(c, up)
			}(c)
		}
	}()
	return l.Addr().String(), asked
}

func fakeHTTPProxy(t *testing.T, auth string) (addr string, asked chan string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	asked = make(chan string, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != "CONNECT" {
					return
				}
				if auth != "" && req.Header.Get("Proxy-Authorization") != auth {
					c.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n\r\n"))
					return
				}
				asked <- req.Host
				up, err := net.Dial("tcp", req.Host)
				if err != nil {
					c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
					return
				}
				defer up.Close()
				c.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
				go io.Copy(up, br)
				io.Copy(c, up)
			}(c)
		}
	}()
	return l.Addr().String(), asked
}

func echoServer(t *testing.T) string {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	return l.Addr().String()
}

func roundTrip(t *testing.T, sv ServerConfig, dest string) error {
	nc, err := dialServer(t.Context(), sv, dest)
	if err != nil {
		return err
	}
	defer nc.Close()
	nc.SetDeadline(time.Now().Add(3 * time.Second))
	nc.Write([]byte("ping\n"))
	line, err := bufio.NewReader(nc).ReadString('\n')
	if err != nil || line != "ping\n" {
		t.Fatalf("echo through proxy: %q %v", line, err)
	}
	return nil
}

func TestProxies(t *testing.T) {
	dest := echoServer(t)
	split := func(a string) (string, int) { h, p, _ := net.SplitHostPort(a); n, _ := strconv.Atoi(p); return h, n }

	sa, asked := fakeSOCKS5(t, "", "")
	h, p := split(sa)
	if err := roundTrip(t, ServerConfig{ProxyType: "socks5", ProxyHost: h, ProxyPort: p}, dest); err != nil {
		t.Fatal(err)
	}
	if got := <-asked; got != dest {
		t.Fatalf("socks asked for %s", got)
	}
	// host names are resolved by the proxy (Tor .onion addresses work)
	sa2, asked2 := fakeSOCKS5(t, "andrew", "s3cret")
	h, p = split(sa2)
	_, dport := split(dest)
	sv := ServerConfig{ProxyType: "socks5", ProxyHost: h, ProxyPort: p, ProxyUser: "andrew", ProxyPass: "s3cret"}
	if err := roundTrip(t, sv, net.JoinHostPort("localhost", strconv.Itoa(dport))); err != nil {
		t.Fatal(err)
	}
	if got := <-asked2; !strings.HasPrefix(got, "localhost:") {
		t.Fatalf("name not passed to the proxy: %s", got)
	}
	sv.ProxyPass = "wrong"
	if _, err := dialServer(t.Context(), sv, dest); err == nil {
		t.Fatal("bad socks password accepted")
	}

	ha, hasked := fakeHTTPProxy(t, "Basic YTpi") // a:b
	h, p = split(ha)
	if err := roundTrip(t, ServerConfig{ProxyType: "http", ProxyHost: h, ProxyPort: p, ProxyUser: "a", ProxyPass: "b"}, dest); err != nil {
		t.Fatal(err)
	}
	<-hasked
	if _, err := dialServer(t.Context(), ServerConfig{ProxyType: "http", ProxyHost: h, ProxyPort: p}, dest); err == nil || !strings.Contains(err.Error(), "407") {
		t.Fatalf("missing proxy password: %v", err)
	}
	// no proxy set: direct
	if err := roundTrip(t, ServerConfig{ProxyType: "socks5", ProxyHost: "", ProxyPort: 1080}, dest); err != nil {
		t.Fatal(err)
	}
}

func TestBackupKeepsProxyPasswordSecret(t *testing.T) {
	a := newBackupApp(t)
	a.SaveServer(ServerConfig{Name: "Tor", Host: "x.onion", Port: 6697, Nick: "a", ProxyType: "socks5", ProxyHost: "127.0.0.1", ProxyPort: 9050, ProxyUser: "u", ProxyPass: "proxypass123"})
	plain := a.ExportSettings(false, "")
	if strings.Contains(plain.Data, "proxypass123") {
		t.Fatal("proxy password in a backup without passwords")
	}
}

func TestSearchHistory(t *testing.T) {
	a := newBackupApp(t)
	sv := a.SaveServer(ServerConfig{Name: "Neo/win", Host: "h", Port: 6667, Nick: "me"})
	sv2 := a.SaveServer(ServerConfig{Name: "Libera", Host: "h2", Port: 6667, Nick: "me"})
	base := time.Now().Add(-time.Hour).UnixMilli()
	put := func(srv ServerConfig, buf, nick, text string, i int) {
		a.logEvent(srv.Name, Event{Type: "msg", Server: srv.ID, Buffer: buf, Kind: "privmsg", Nick: nick, Text: text, Time: base + int64(i)*1000})
	}
	for i := 0; i < 50; i++ {
		put(sv, "#lounge", "bob", "filler line "+strconv.Itoa(i), i)
	}
	put(sv, "#lounge", "Caleo", "the \x02Raspberry\x02 Pi arrived", 60)
	put(sv2, "#linux", "flux", "raspberry pi 5 kernel panic", 61)
	put(sv2, "#linux", "flux", "nothing to see", 62)
	r := a.SearchHistory("RASPBERRY pi", "", "")
	if len(r.Hits) != 2 || r.Hits[0].Buffer != "#linux" || r.Hits[1].ServerName != "Neo/win" {
		t.Fatalf("%+v", r.Hits)
	}
	if r := a.SearchHistory("raspberry", sv2.ID, ""); len(r.Hits) != 1 {
		t.Fatalf("server filter: %d", len(r.Hits))
	}
	if r := a.SearchHistory("caleo arrived", "", "#LOUNGE"); len(r.Hits) != 1 {
		t.Fatal("nick + buffer filter")
	}
	if r := a.SearchHistory("raspberry kernel", "", ""); len(r.Hits) != 1 {
		t.Fatal("all words must match")
	}
	ctx := a.HistoryAround(sv.ID, "#lounge", base+60000, 3)
	if len(ctx) != 4 || ctx[3].Nick != "Caleo" || ctx[0].Text != "filler line 47" {
		t.Fatalf("context %+v", ctx)
	}
	os.RemoveAll(filepath.Join(a.store.dir, "logs"))
	if r := a.SearchHistory("raspberry", "", ""); len(r.Hits) != 0 {
		t.Fatal("no logs")
	}
}

func TestPerformWait(t *testing.T) {
	for in, want := range map[string]time.Duration{"/wait 5": 5 * time.Second, "/WAIT 1.5": 1500 * time.Millisecond, "/delay 2s": 2 * time.Second, "/sleep": time.Second, "/wait 999": performWaitMax} {
		if got, ok := performWait(in); !ok || got != want {
			t.Errorf("%q = %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"/msg bot !invite", "/wait abc", "/wait -3", "wait 5"} {
		if _, ok := performWait(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}
