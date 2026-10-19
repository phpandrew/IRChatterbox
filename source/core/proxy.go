package core

// Connecting through a proxy: SOCKS5 (Tor, SSH -D, most VPN apps) or an HTTP proxy that
// allows CONNECT. Set per server in the server editor → Advanced. With SOCKS5 the proxy
// looks up the server's name itself, so .onion addresses work through Tor.

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// proxyAddr returns the proxy's host:port, or "" when the server connects directly.
func (s ServerConfig) proxyAddr() string {
	if s.ProxyType == "" || s.ProxyType == "none" || strings.TrimSpace(s.ProxyHost) == "" || s.ProxyPort <= 0 || s.ProxyPort > 65535 {
		return ""
	}
	return net.JoinHostPort(strings.TrimSpace(s.ProxyHost), strconv.Itoa(s.ProxyPort))
}

// dialServer opens the TCP connection to addr (host:port), through the proxy if one is set.
func dialServer(ctx context.Context, s ServerConfig, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 60 * time.Second}
	paddr := s.proxyAddr()
	if paddr == "" {
		return d.DialContext(ctx, "tcp", addr)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch s.ProxyType {
	case "socks5":
		var auth *proxy.Auth
		if s.ProxyUser != "" || s.ProxyPass != "" {
			auth = &proxy.Auth{User: s.ProxyUser, Password: s.ProxyPass}
		}
		pd, err := proxy.SOCKS5("tcp", paddr, auth, d)
		if err != nil {
			return nil, err
		}
		nc, err := pd.(proxy.ContextDialer).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, proxyErr(paddr, err)
		}
		return nc, nil
	case "http":
		nc, err := d.DialContext(ctx, "tcp", paddr)
		if err != nil {
			return nil, proxyErr(paddr, err)
		}
		if err := httpConnect(ctx, nc, addr, s.ProxyUser, s.ProxyPass); err != nil {
			nc.Close()
			return nil, err
		}
		return nc, nil
	}
	return nil, fmt.Errorf("unknown proxy type %q", s.ProxyType)
}

func proxyErr(paddr string, err error) error {
	return fmt.Errorf("through the proxy at %s: %w", paddr, err)
}

// httpConnect asks an HTTP proxy to open a tunnel to addr.
func httpConnect(ctx context.Context, nc net.Conn, addr, user, pass string) error {
	if dl, ok := ctx.Deadline(); ok {
		nc.SetDeadline(dl)
		defer nc.SetDeadline(time.Time{})
	}
	req := "CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\nUser-Agent: IRChatterbox/" + appVersion + "\r\n"
	if user != "" || pass != "" {
		req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass)) + "\r\n"
	}
	if _, err := nc.Write([]byte(req + "\r\n")); err != nil {
		return err
	}
	br := bufio.NewReader(nc)
	resp, err := http.ReadResponse(br, &http.Request{Method: "CONNECT"})
	if err != nil {
		return errors.New("the proxy didn't answer like an HTTP proxy")
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == 407 {
			return errors.New("the proxy wants a user name and password (407)")
		}
		return fmt.Errorf("the proxy refused the connection: %s", resp.Status)
	}
	if br.Buffered() > 0 {
		return errors.New("the proxy sent unexpected data")
	}
	return nil
}
