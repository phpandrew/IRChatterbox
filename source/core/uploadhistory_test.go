package core

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// historyHost is a fake image host over TLS that answers uploads and deletes for every kind
// of host, the way each one does.
type historyHost struct {
	srv     *httptest.Server
	deleted []string
	n       int
}

func newHistoryHost(t *testing.T) *historyHost {
	h := &historyHost{}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/upload":
			io.WriteString(w, `{"ok":true,"id":"k3x9Qa2mZp","url":"https://i.example/k3x9Qa2mZp.webp","expires":"2099-01-01T00:00:00Z",`+
				`"delete_token":"tok123","delete_url":"`+h.srv.URL+`/delete"}`)
		case r.URL.Path == "/delete":
			if r.FormValue("id") != "k3x9Qa2mZp" || r.FormValue("token") != "tok123" {
				w.WriteHeader(403)
				io.WriteString(w, `{"ok":false,"error":"That delete code doesn't match the image."}`)
				return
			}
			h.deleted = append(h.deleted, "own:"+r.FormValue("id"))
			io.WriteString(w, `{"ok":true}`)
		case r.URL.Path == "/3/image" && r.Method == http.MethodPost:
			io.WriteString(w, `{"data":{"link":"https://i.imgur.com/abc123.png","deletehash":"DelHash9"},"success":true,"status":200}`)
		case strings.HasPrefix(r.URL.Path, "/3/image/") && r.Method == http.MethodDelete:
			if r.Header.Get("Authorization") != "Client-ID cid" {
				w.WriteHeader(403)
				return
			}
			h.deleted = append(h.deleted, "imgur:"+strings.TrimPrefix(r.URL.Path, "/3/image/"))
			io.WriteString(w, `{"data":true,"success":true,"status":200}`)
		case r.URL.Path == "/user/api.php":
			switch r.FormValue("reqtype") {
			case "fileupload":
				h.n++
				io.WriteString(w, fmt.Sprintf("https://files.catbox.moe/q1w2e%d.png", h.n))
			case "deletefiles":
				if r.FormValue("userhash") != "hash1" {
					io.WriteString(w, "Invalid userhash")
					return
				}
				h.deleted = append(h.deleted, "catbox:"+r.FormValue("files"))
				io.WriteString(w, "Files successfully deleted.")
			}
		default:
			w.WriteHeader(404)
		}
	}))
	oldA, oldB, oldC, oldClient := irchatterboxUploadURL, imgurUploadURL, catboxUploadURL, uploadClient
	irchatterboxUploadURL, imgurUploadURL, catboxUploadURL = h.srv.URL+"/upload", h.srv.URL+"/3/image", h.srv.URL+"/user/api.php"
	uploadClient = h.srv.Client()
	t.Cleanup(func() {
		h.srv.Close()
		irchatterboxUploadURL, imgurUploadURL, catboxUploadURL, uploadClient = oldA, oldB, oldC, oldClient
	})
	return h
}

func TestUploadHistoryAndDelete(t *testing.T) {
	a := newBackupApp(t)
	h := newHistoryHost(t)
	upload := func(host, catboxHash string) {
		t.Helper()
		a.store.Update(func(c *Config) {
			c.Settings.UploadHost, c.Settings.ImgurClientID, c.Settings.CatboxUserHash = host, "cid", catboxHash
		})
		if _, err := a.UploadImage(testPNG(t)); err != nil {
			t.Fatalf("%s: %v", host, err)
		}
	}
	upload(UploadHostIRChatterbox, "")
	upload(UploadHostImgur, "")
	upload(UploadHostCatbox, "") // anonymous: can't be deleted
	upload(UploadHostCatbox, "hash1")

	list := a.GetUploadHistory()
	if len(list) != 4 {
		t.Fatalf("history has %d items, want 4", len(list))
	}
	if list[0].Host != UploadHostCatbox || !list[0].CanDelete || list[1].CanDelete || list[1].NoDelete != "anonymous" {
		t.Fatalf("catbox rows: %+v / %+v", list[0], list[1])
	}
	if !list[2].CanDelete || !list[3].CanDelete || list[3].Expires == "" {
		t.Fatalf("imgur/irchatterbox rows: %+v / %+v", list[2], list[3])
	}

	// the delete codes stay out of what the UI gets, and the file is private
	raw, _ := os.ReadFile(a.uploadHistoryPath())
	if !strings.Contains(string(raw), "tok123") {
		t.Fatal("delete code wasn't saved")
	}
	if st, _ := os.Stat(a.uploadHistoryPath()); st != nil && st.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("history file mode %v", st.Mode())
	}

	if msg := a.DeleteUpload(list[1].URL); !strings.Contains(msg, "userhash") {
		t.Fatalf("anonymous catbox delete: %q", msg)
	}
	for _, it := range []UploadItem{list[0], list[2], list[3]} {
		if msg := a.DeleteUpload(it.URL); msg != "" {
			t.Fatalf("delete %s: %s", it.Host, msg)
		}
	}
	want := "catbox:q1w2e2.png imgur:DelHash9 own:k3x9Qa2mZp"
	if got := strings.Join(h.deleted, " "); got != want {
		t.Fatalf("deleted %q, want %q", got, want)
	}
	if left := a.GetUploadHistory(); len(left) != 1 || left[0].URL != list[1].URL {
		t.Fatalf("left in history: %+v", left)
	}
	a.ForgetUpload(list[1].URL)
	if len(a.GetUploadHistory()) != 0 {
		t.Fatal("forget didn't remove it")
	}
}

func TestUploadHistoryDeleteFailuresKeepTheRow(t *testing.T) {
	a := newBackupApp(t)
	newHistoryHost(t)
	a.store.Update(func(c *Config) { c.Settings.UploadHost, c.Settings.ImgurClientID = UploadHostImgur, "cid" })
	a.UploadImage(testPNG(t))
	a.store.Update(func(c *Config) { c.Settings.ImgurClientID = "wrong" })
	link := a.GetUploadHistory()[0].URL
	if msg := a.DeleteUpload(link); !strings.Contains(msg, "Client ID") {
		t.Fatalf("got %q", msg)
	}
	if len(a.GetUploadHistory()) != 1 {
		t.Fatal("a failed delete removed the row")
	}
}

func TestUploadHistoryExpired(t *testing.T) {
	r := uploadRecord{URL: "https://i.example/x.webp", Host: UploadHostIRChatterbox, Expires: "2020-01-01T00:00:00Z", DeleteToken: "t", ImageID: "x", DeleteURL: "https://i.example/delete"}
	it := r.item(time.Now())
	if !it.Expired || it.CanDelete || it.NoDelete != "expired" {
		t.Fatalf("%+v", it)
	}
}

// A delete link pointing at another server than the upload is ignored.
func TestUploadIgnoresForeignDeleteURL(t *testing.T) {
	withUploadServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true,"id":"k3x9Qa2mZp","url":"https://i.example/k3x9Qa2mZp.webp","delete_token":"t","delete_url":"https://evil.example/delete"}`)
	})
	res, err := uploadImage(UploadHostIRChatterbox, "", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.del.Token != "" {
		t.Fatalf("kept a foreign delete link: %+v", res.del)
	}
}
