package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "image/gif"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Inline image thumbnails. A full-size picture decoded in the chat costs its width × height × 4
// bytes for as long as the page lives (a 1000×750 upload is ~3 MB), and WebView keeps decoded
// images cached long after they scroll away. So the chat shows a copy shrunk to the size it's
// drawn at (480×320 CSS pixels, at 1.5× for high-DPI screens), made here and kept in a disk
// cache; the popup still opens the original. Anything we can't or needn't shrink (already
// small, animated, a format Go can't read) comes back as "" and the chat loads the original.

const (
	thumbMaxW      = 720
	thumbMaxH      = 480
	thumbMaxBytes  = 15 << 20   // biggest download we'll shrink
	thumbMaxPixels = 40_000_000 // decompression-bomb guard
	thumbCacheMax  = 200 << 20  // disk cache: trimmed back to 150 MB past this
	thumbCacheKeep = 150 << 20
	thumbParallel  = 4
)

var (
	thumbClient = &http.Client{
		Timeout: 25 * time.Second,
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 6 * time.Second, Control: publicOnly}).DialContext,
			TLSHandshakeTimeout:   6 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConns:          8,
			IdleConnTimeout:       60 * time.Second,
		},
		CheckRedirect: previewClient.CheckRedirect,
	}
	thumbSem      = make(chan struct{}, thumbParallel)
	thumbMu       sync.Mutex
	thumbInflight = map[string]*thumbCall{}
	thumbWrites   int
	errNoThumb    = errors.New("no thumbnail")
)

type thumbCall struct {
	done chan struct{}
	url  string
}

// GetThumbnail returns a data: URL of a shrunk copy of the image at link, or "" to show the
// original instead.
func (a *App) GetThumbnail(link string) string {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(link) > 2048 {
		return ""
	}
	dir := a.thumbDir()
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(link))
	key := hex.EncodeToString(sum[:16])
	if out, ok := readThumb(dir, key); ok {
		return out
	}

	thumbMu.Lock()
	if c, ok := thumbInflight[key]; ok { // the same image asked for twice (split view, re-render)
		thumbMu.Unlock()
		<-c.done
		return c.url
	}
	c := &thumbCall{done: make(chan struct{})}
	thumbInflight[key] = c
	thumbMu.Unlock()
	defer func() {
		thumbMu.Lock()
		delete(thumbInflight, key)
		thumbMu.Unlock()
		close(c.done)
	}()

	thumbSem <- struct{}{}
	data, ext, err := makeThumb(link)
	<-thumbSem
	switch {
	case errors.Is(err, errNoThumb):
		writeThumbFile(dir, key+".none", []byte("-")) // remembered, so it isn't fetched again
		return ""
	case err != nil:
		return "" // network trouble: try again next time
	}
	writeThumbFile(dir, key+"."+ext, data)
	c.url = dataURL(ext, data)
	a.trimThumbsSometimes(dir)
	return c.url
}

func (a *App) thumbDir() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = filepath.Join(a.store.dir, "cache")
	} else {
		base = filepath.Join(base, "IRChatterbox")
	}
	dir := filepath.Join(base, "thumbs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	return dir
}

func readThumb(dir, key string) (string, bool) {
	if _, err := os.Stat(filepath.Join(dir, key+".none")); err == nil {
		return "", true
	}
	for _, ext := range []string{"jpg", "png"} {
		p := filepath.Join(dir, key+"."+ext)
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			now := time.Now()
			os.Chtimes(p, now, now) // recently used: trimmed last
			return dataURL(ext, b), true
		}
	}
	return "", false
}

func writeThumbFile(dir, name string, data []byte) {
	tmp := filepath.Join(dir, name+".tmp")
	if os.WriteFile(tmp, data, 0o600) == nil {
		os.Rename(tmp, filepath.Join(dir, name))
	}
}

func dataURL(ext string, b []byte) string {
	mime := "image/jpeg"
	if ext == "png" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// makeThumb downloads and shrinks one image. errNoThumb means "show the original".
func makeThumb(link string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, link, nil)
	if err != nil {
		return nil, "", errNoThumb
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; IRChatterbox/"+appVersion+")")
	req.Header.Set("Accept", "image/webp,image/png,image/jpeg,image/*;q=0.8")
	resp, err := thumbClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests:
		return nil, "", errNoThumb // gone or forbidden: the original won't load either
	default:
		return nil, "", errors.New(resp.Status) // busy or broken: try again another time
	}
	if resp.ContentLength > thumbMaxBytes {
		return nil, "", errNoThumb
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, thumbMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > thumbMaxBytes {
		return nil, "", errNoThumb
	}
	return shrinkImage(raw)
}

// shrinkImage fits raw inside thumbMaxW × thumbMaxH. errNoThumb when it's already small enough,
// animated (GIF, animated PNG or WebP), too big, or not something Go can read.
func shrinkImage(raw []byte) ([]byte, string, error) {
	if bytes.HasPrefix(raw, []byte("GIF8")) || isAPNG(raw) {
		return nil, "", errNoThumb // may be animated: the original keeps moving
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > thumbMaxPixels {
		return nil, "", errNoThumb
	}
	if cfg.Width <= thumbMaxW && cfg.Height <= thumbMaxH {
		return nil, "", errNoThumb
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", errNoThumb // animated WebP lands here too
	}
	scale := min(float64(thumbMaxW)/float64(cfg.Width), float64(thumbMaxH)/float64(cfg.Height))
	w, h := max(1, int(float64(cfg.Width)*scale+0.5)), max(1, int(float64(cfg.Height)*scale+0.5))
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)

	var out bytes.Buffer
	if dst.Opaque() {
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", errNoThumb
		}
		return out.Bytes(), "jpg", nil
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&out, dst); err != nil {
		return nil, "", errNoThumb
	}
	return out.Bytes(), "png", nil
}

// isAPNG spots an animated PNG (an acTL chunk before the image data).
func isAPNG(b []byte) bool {
	if !bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) {
		return false
	}
	head := b[:min(len(b), 4096)]
	i := bytes.Index(head, []byte("acTL"))
	return i >= 0 && (bytes.Index(head, []byte("IDAT")) < 0 || i < bytes.Index(head, []byte("IDAT")))
}

// trimThumbsSometimes keeps the cache under thumbCacheMax, oldest-used first.
func (a *App) trimThumbsSometimes(dir string) {
	thumbMu.Lock()
	thumbWrites++
	n := thumbWrites
	thumbMu.Unlock()
	if n%50 != 1 { // at the first write of a session, then every 50
		return
	}
	go trimThumbs(dir, thumbCacheMax, thumbCacheKeep)
}

func trimThumbs(dir string, maxBytes, keepBytes int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type f struct {
		path string
		size int64
		mod  time.Time
	}
	var files []f
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if strings.HasSuffix(e.Name(), ".tmp") && time.Since(info.ModTime()) > time.Hour {
			os.Remove(filepath.Join(dir, e.Name()))
			continue
		}
		size := max(info.Size(), 4096) // what a file really takes on disk, so markers count too
		files = append(files, f{filepath.Join(dir, e.Name()), size, info.ModTime()})
		total += size
	}
	if total <= maxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, x := range files {
		if total <= keepBytes {
			break
		}
		if os.Remove(x.path) == nil {
			total -= x.size
		}
	}
}
