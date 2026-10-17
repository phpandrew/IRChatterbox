package core

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testPNG(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

// withUploadServer points both hosts at a local server for one test.
func withUploadServer(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	oldA, oldB := irchatterboxUploadURL, imgurUploadURL
	irchatterboxUploadURL, imgurUploadURL = srv.URL+"/upload", srv.URL+"/3/image"
	t.Cleanup(func() { srv.Close(); irchatterboxUploadURL, imgurUploadURL = oldA, oldB })
}

func TestUploadIRChatterbox(t *testing.T) {
	var gotName, gotUA string
	var gotBytes []byte
	withUploadServer(t, func(w http.ResponseWriter, r *http.Request) {
		f, hdr, err := r.FormFile("image")
		if err != nil {
			t.Errorf("no image field: %v", err)
			return
		}
		gotName, gotUA = hdr.Filename, r.UserAgent()
		gotBytes, _ = io.ReadAll(f)
		io.WriteString(w, `{"ok":true,"id":"k3x9Qa2mZp","url":"https://i.irchatterbox.com/k3x9Qa2mZp.webp","expires":"2026-10-24T18:00:00Z"}`)
	})
	data := testPNG(t)
	res, err := uploadImage("", "", "data:image/png;base64,"+data)
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://i.irchatterbox.com/k3x9Qa2mZp.webp" || res.Expires == "" || res.Host != UploadHostIRChatterbox {
		t.Fatalf("got %+v", res)
	}
	raw, _ := base64.StdEncoding.DecodeString(data)
	if !bytes.Equal(gotBytes, raw) {
		t.Error("uploaded bytes differ from the original")
	}
	if gotName != "image.png" {
		t.Errorf("file name %q: the user's own file name must not be sent", gotName)
	}
	if !strings.HasPrefix(gotUA, "IRChatterbox/") {
		t.Errorf("user agent %q", gotUA)
	}
}

func TestUploadIRChatterboxErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{415, `{"ok":false,"error":"Only JPG, PNG, WebP and BMP images can be uploaded."}`, "Only JPG"},
		{429, `<html>429 Too Many Requests</html>`, "Too many uploads"},
		{502, `<html>Bad Gateway</html>`, "had a problem (502"},
		{200, `{"ok":true,"url":"javascript:alert(1)"}`, "unexpected reply"},
		{200, `not json`, "had a problem"},
	}
	for _, c := range cases {
		withUploadServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			io.WriteString(w, c.body)
		})
		_, err := uploadImage(UploadHostIRChatterbox, "", testPNG(t))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%d %s: got %v, want %q", c.status, c.body, err, c.want)
		}
	}
}

func TestUploadImgur(t *testing.T) {
	var auth, typ string
	withUploadServer(t, func(w http.ResponseWriter, r *http.Request) {
		auth, typ = r.Header.Get("Authorization"), r.FormValue("type")
		if _, _, err := r.FormFile("image"); err != nil {
			t.Errorf("no image field: %v", err)
		}
		io.WriteString(w, `{"data":{"link":"https://i.imgur.com/abc123.png"},"success":true,"status":200}`)
	})
	res, err := uploadImage(UploadHostImgur, "  myclientid ", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://i.imgur.com/abc123.png" || res.Expires != "" || res.Host != UploadHostImgur {
		t.Fatalf("got %+v", res)
	}
	if auth != "Client-ID myclientid" || typ != "file" {
		t.Errorf("auth %q type %q", auth, typ)
	}
}

func TestUploadImgurErrors(t *testing.T) {
	if _, err := uploadImage(UploadHostImgur, "", testPNG(t)); err == nil || !strings.Contains(err.Error(), "Client ID") {
		t.Errorf("missing client id: %v", err)
	}
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{403, `{"data":{"error":"Invalid client_id"},"success":false,"status":403}`, "didn't accept the Client ID"},
		{400, `{"data":{"error":{"message":"File type invalid (1)"}},"success":false,"status":400}`, "Imgur: File type invalid"},
		{400, `{"data":{"error":"Image is larger than 20MB"},"success":false,"status":400}`, "Imgur: Image is larger"},
		{500, `oops`, "Imgur had a problem"},
	}
	for _, c := range cases {
		withUploadServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			io.WriteString(w, c.body)
		})
		_, err := uploadImage(UploadHostImgur, "id", testPNG(t))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%d: got %v, want %q", c.status, err, c.want)
		}
	}
}

func TestUploadRejectsBeforeSending(t *testing.T) {
	sent := false
	withUploadServer(t, func(w http.ResponseWriter, r *http.Request) { sent = true })

	var g bytes.Buffer
	gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black}), nil)
	big := make([]byte, maxUploadBytes+1)
	copy(big, "\x89PNG\r\n\x1a\n")
	for name, c := range map[string]struct{ data, want string }{
		"gif":     {base64.StdEncoding.EncodeToString(g.Bytes()), "Only JPG"},
		"text":    {base64.StdEncoding.EncodeToString([]byte("hello, not an image")), "Only JPG"},
		"empty":   {"", "couldn't be read"},
		"garbage": {"%%%not base64", "couldn't be read"},
		"toobig":  {base64.StdEncoding.EncodeToString(big), "at most 10 MB"},
	} {
		if _, err := uploadImage("", "", c.data); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", name, err, c.want)
		}
	}
	if _, err := uploadImage("dropbox", "", testPNG(t)); err == nil || !strings.Contains(err.Error(), "Unknown image host") {
		t.Errorf("unknown host: %v", err)
	}
	if sent {
		t.Error("a rejected file was sent to the server")
	}
}

func TestUploadAcceptsAllFormats(t *testing.T) {
	withUploadServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true,"url":"https://i.irchatterbox.com/aaaaaaaaaa.webp"}`)
	})
	for name, head := range map[string]string{
		"jpg":  "\xff\xd8\xff\xe0\x00\x10JFIF\x00",
		"webp": "RIFF\x24\x00\x00\x00WEBPVP8 ",
		"bmp":  "BM\x36\x00\x00\x00\x00\x00\x00\x00\x36\x00\x00\x00",
	} {
		data := base64.StdEncoding.EncodeToString([]byte(head + strings.Repeat("\x00", 64)))
		if _, err := uploadImage("", "", data); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
