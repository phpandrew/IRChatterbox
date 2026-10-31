package core

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testImage(w, h int, alpha uint8) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), 90, alpha})
		}
	}
	return im
}

func encodePNG(t *testing.T, im image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decodedSize(t *testing.T, data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

func TestShrinkImage(t *testing.T) {
	var jb bytes.Buffer
	jpeg.Encode(&jb, testImage(2000, 1500, 255), nil)
	out, ext, err := shrinkImage(jb.Bytes())
	if err != nil || ext != "jpg" {
		t.Fatalf("photo: %v %q", err, ext)
	}
	if w, h := decodedSize(t, out); w != 640 || h != 480 {
		t.Fatalf("photo shrunk to %dx%d, want 640x480", w, h)
	}

	// transparency is kept: PNG out
	out, ext, err = shrinkImage(encodePNG(t, testImage(1600, 400, 120)))
	if err != nil || ext != "png" {
		t.Fatalf("transparent: %v %q", err, ext)
	}
	if w, h := decodedSize(t, out); w != 720 || h != 180 {
		t.Fatalf("transparent shrunk to %dx%d, want 720x180", w, h)
	}

	var gb bytes.Buffer
	gif.Encode(&gb, image.NewPaletted(image.Rect(0, 0, 1200, 900), color.Palette{color.Black, color.White}), nil)
	apng := encodePNG(t, testImage(1200, 900, 255))
	apng = append(apng[:33:33], append([]byte("\x00\x00\x00\x08acTL\x00\x00\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00"), apng[33:]...)...)
	bomb := encodePNG(t, image.NewGray(image.Rect(0, 0, 10, 10)))
	copy(bomb[16:24], []byte{0, 0, 0x4e, 0x20, 0, 0, 0x4e, 0x20}) // claims 20000×20000

	for name, raw := range map[string][]byte{
		"already small": encodePNG(t, testImage(700, 400, 255)),
		"gif":           gb.Bytes(),
		"animated png":  apng,
		"huge":          bomb,
		"not an image":  []byte("<html>nope</html>"),
	} {
		if _, _, err := shrinkImage(raw); !errors.Is(err, errNoThumb) {
			t.Errorf("%s: got %v, want errNoThumb", name, err)
		}
	}
}

func TestGetThumbnailCaches(t *testing.T) {
	a := newBackupApp(t)
	os.Setenv("XDG_CACHE_HOME", t.TempDir())
	os.Setenv("LOCALAPPDATA", os.Getenv("XDG_CACHE_HOME"))
	var hits atomic.Int32
	big := encodePNG(t, testImage(1500, 1000, 255))
	small := encodePNG(t, testImage(100, 100, 255))
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/big.png":
			w.Write(big)
		case "/small.png":
			w.Write(small)
		case "/busy.png":
			w.WriteHeader(503)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	old := thumbClient
	thumbClient = srv.Client()
	defer func() { thumbClient = old }()

	got := a.GetThumbnail(srv.URL + "/big.png")
	if !strings.HasPrefix(got, "data:image/jpeg;base64,") {
		t.Fatalf("big: got %.40q", got)
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, "data:image/jpeg;base64,"))
	if w, h := decodedSize(t, raw); w != 720 || h != 480 {
		t.Fatalf("thumbnail is %dx%d", w, h)
	}
	if again := a.GetThumbnail(srv.URL + "/big.png"); again != got || hits.Load() != 1 {
		t.Fatalf("second ask fetched again (%d hits)", hits.Load())
	}
	// small and missing images: "" (show the original), remembered
	for _, p := range []string{"/small.png", "/gone.png"} {
		if a.GetThumbnail(srv.URL+p) != "" || a.GetThumbnail(srv.URL+p) != "" {
			t.Fatalf("%s: want empty", p)
		}
	}
	if hits.Load() != 3 {
		t.Fatalf("small/gone were fetched again: %d hits", hits.Load())
	}
	// a busy server isn't remembered: tried again next time
	a.GetThumbnail(srv.URL + "/busy.png")
	a.GetThumbnail(srv.URL + "/busy.png")
	if hits.Load() != 5 {
		t.Fatalf("busy: %d hits, want 5", hits.Load())
	}
	for _, bad := range []string{"", "file:///etc/passwd", "javascript:alert(1)", "https://"} {
		if a.GetThumbnail(bad) != "" {
			t.Fatalf("%q gave a thumbnail", bad)
		}
	}
}

func TestThumbClientRefusesLocalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("x")) }))
	defer srv.Close()
	if _, _, err := makeThumb(srv.URL + "/x.png"); err == nil || errors.Is(err, errNoThumb) {
		t.Fatalf("fetched from a local address: %v", err)
	}
}

func TestTrimThumbs(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		p := filepath.Join(dir, string(rune('a'+i))+".jpg")
		os.WriteFile(p, make([]byte, 10_000), 0o600)
		ts := time.Now().Add(time.Duration(i-10) * time.Hour)
		os.Chtimes(p, ts, ts)
	}
	trimThumbs(dir, 50_000, 30_000)
	left, _ := os.ReadDir(dir)
	if len(left) != 3 {
		t.Fatalf("%d files left, want the 3 newest", len(left))
	}
	if _, err := os.Stat(filepath.Join(dir, "j.jpg")); err != nil {
		t.Fatal("the newest file was removed")
	}
}
