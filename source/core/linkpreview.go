package core

// Link previews: the title, description and picture a web page declares for sharing
// (OpenGraph / Twitter card tags, or its <title>), shown under links in chat.
//
// Pages are fetched by the app, never by the chat view, and only from public internet
// addresses: a link posted in a channel can't make the app poke at your router or other
// machines on your network.

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

type LinkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	Site        string `json:"site,omitempty"`
	Error       string `json:"error,omitempty"`
}

type previewEntry struct {
	p  LinkPreview
	at time.Time
}

var (
	previewMu    sync.Mutex
	previewCache = map[string]previewEntry{}
	previewSlots = make(chan struct{}, 4) // at most 4 pages fetched at once
)

const (
	previewTTL      = 30 * time.Minute
	previewMaxBytes = 768 * 1024
	previewMaxCache = 500
)

// errPrivateAddr: the link points at this computer or the local network.
var errPrivateAddr = errors.New("not a public address")

// publicOnly refuses connections to loopback, private, link-local and other non-public
// addresses. It runs after DNS, for every connection (redirects included).
func publicOnly(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !isPublicIP(ip) {
		return errPrivateAddr
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64/10 carrier-grade NAT, 0/8, 192.0.0/24, 198.18/15 benchmarking, 240/4 reserved
		switch {
		case v4[0] == 0, v4[0] == 100 && v4[1]&0xc0 == 64, v4[0] == 192 && v4[1] == 0 && v4[2] == 0,
			v4[0] == 198 && (v4[1] == 18 || v4[1] == 19), v4[0] >= 240:
			return false
		}
	}
	return true
}

var previewClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 6 * time.Second, Control: publicOnly}).DialContext,
		TLSHandshakeTimeout:   6 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		MaxIdleConns:          8,
		IdleConnTimeout:       60 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("unsupported redirect")
		}
		return nil
	},
}

// GetLinkPreview returns the preview for a web page (cached for a while).
func (a *App) GetLinkPreview(raw string) LinkPreview {
	if !a.store.Get().Settings.LinkPreviews {
		return LinkPreview{URL: raw, Error: "off"}
	}
	return fetchPreview(context.Background(), raw)
}

func fetchPreview(ctx context.Context, raw string) LinkPreview {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(raw) > 2048 {
		return LinkPreview{URL: raw, Error: "unsupported link"}
	}
	key := u.String()
	previewMu.Lock()
	if e, ok := previewCache[key]; ok && time.Since(e.at) < previewTTL {
		previewMu.Unlock()
		return e.p
	}
	previewMu.Unlock()

	select {
	case previewSlots <- struct{}{}:
		defer func() { <-previewSlots }()
	case <-ctx.Done():
		return LinkPreview{URL: raw, Error: "cancelled"}
	}
	p := loadPreview(ctx, u)
	previewMu.Lock()
	if len(previewCache) >= previewMaxCache {
		for k, e := range previewCache {
			if time.Since(e.at) >= previewTTL || len(previewCache) >= previewMaxCache {
				delete(previewCache, k)
			}
		}
	}
	previewCache[key] = previewEntry{p, time.Now()}
	previewMu.Unlock()
	return p
}

func loadPreview(ctx context.Context, u *url.URL) LinkPreview {
	p := LinkPreview{URL: u.String()}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	// many sites only serve their sharing tags to link-preview bots
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; IRChatterbox/"+appVersion+"; +https://irchatterbox.com) facebookexternalhit/1.1")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "en;q=0.8, *;q=0.5")
	resp, err := previewClient.Do(req)
	if err != nil {
		if errors.Is(err, errPrivateAddr) {
			p.Error = "local address"
		} else {
			p.Error = "unreachable"
		}
		return p
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		p.Error = resp.Status
		return p
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "html") {
		p.Error = "not a web page"
		return p
	}
	body, err := charset.NewReader(io.LimitReader(resp.Body, previewMaxBytes), ct)
	if err != nil {
		body = io.LimitReader(resp.Body, previewMaxBytes)
	}
	parsePreview(body, resp.Request.URL, &p)
	if p.Title == "" && p.Description == "" {
		p.Error = "no preview"
	}
	return p
}

// parsePreview reads the <head> of a page for its sharing tags.
func parsePreview(r io.Reader, base *url.URL, p *LinkPreview) {
	z := html.NewTokenizer(r)
	meta := map[string]string{}
	var title strings.Builder
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			finishPreview(meta, title.String(), base, p)
			return
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tag == "body" {
				finishPreview(meta, title.String(), base, p)
				return
			}
			if tag == "title" && title.Len() == 0 {
				inTitle = true
			}
			if tag != "meta" || !hasAttr {
				continue
			}
			var key, content string
			for {
				k, v, more := z.TagAttr()
				switch strings.ToLower(string(k)) {
				case "property", "name", "itemprop":
					if key == "" {
						key = strings.ToLower(strings.TrimSpace(string(v)))
					}
				case "content":
					content = strings.TrimSpace(string(v))
				}
				if !more {
					break
				}
			}
			if key != "" && content != "" {
				if _, seen := meta[key]; !seen {
					meta[key] = content
				}
			}
		case html.TextToken:
			if inTitle && title.Len() < 400 {
				title.Write(z.Text())
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "title" {
				inTitle = false
			} else if string(name) == "head" {
				finishPreview(meta, title.String(), base, p)
				return
			}
		}
	}
}

func finishPreview(meta map[string]string, title string, base *url.URL, p *LinkPreview) {
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := meta[k]; v != "" {
				return v
			}
		}
		return ""
	}
	p.Title = clipRunes(collapseSpace(first("og:title", "twitter:title")), 200)
	if p.Title == "" {
		p.Title = clipRunes(collapseSpace(html.UnescapeString(title)), 200)
	}
	p.Description = clipRunes(collapseSpace(first("og:description", "twitter:description", "description")), 300)
	p.Site = clipRunes(collapseSpace(first("og:site_name", "application-name")), 60)
	if p.Site == "" {
		p.Site = strings.TrimPrefix(base.Hostname(), "www.")
	}
	if img := first("og:image:secure_url", "og:image", "og:image:url", "twitter:image", "twitter:image:src", "image"); img != "" {
		if iu, err := base.Parse(img); err == nil && (iu.Scheme == "https" || iu.Scheme == "http") && len(iu.String()) <= 2048 {
			p.Image = iu.String()
		}
	}
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
