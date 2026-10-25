package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSyncServer behaves like account/api.php: email codes (always 123456), tokens, one
// file per device, versions that only go up, and only age-encrypted uploads accepted.
type fakeSyncServer struct {
	mu      sync.Mutex
	tokens  map[string]string                  // token → email
	files   map[string]map[string]fakeSyncFile // email → device → file
	version int64
	puts    int
}

type fakeSyncFile struct {
	data    []byte
	version int64
}

func newFakeSyncServer(t *testing.T) (*fakeSyncServer, string) {
	f := &fakeSyncServer{tokens: map[string]string{}, files: map[string]map[string]fakeSyncFile{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL + "/account/api.php"
}

func (f *fakeSyncServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(status int, v any) {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(v)
	}
	var in map[string]string
	op := r.URL.Query().Get("op")
	if r.Header.Get("Content-Type") == "application/json" {
		json.NewDecoder(r.Body).Decode(&in)
	}
	email := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	switch r.Method + " " + op {
	case "POST code":
		reply(200, map[string]string{"pending": "p-" + in["email"]})
		return
	case "POST verify":
		if in["code"] != "123456" {
			reply(400, map[string]string{"error": "That code isn't right."})
			return
		}
		tok := "tok" + strconv.Itoa(len(f.tokens)) + strings.Repeat("x", 30)
		f.tokens[tok] = strings.TrimPrefix(in["pending"], "p-")
		reply(200, map[string]string{"token": tok, "email": f.tokens[tok]})
		return
	}
	if email == "" {
		reply(401, map[string]string{"error": "Not signed in"})
		return
	}
	dev := r.URL.Query().Get("device")
	switch r.Method + " " + op {
	case "GET sync":
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		var devices []map[string]any
		var files []map[string]any
		var v int64
		for d, x := range f.files[email] {
			devices = append(devices, map[string]any{"device": d, "version": x.version})
			v = max(v, x.version)
			if x.version > since {
				files = append(files, map[string]any{"device": d, "version": x.version, "data": base64.StdEncoding.EncodeToString(x.data)})
			}
		}
		reply(200, map[string]any{"version": v, "devices": devices, "files": files})
	case "PUT sync":
		b, _ := io.ReadAll(r.Body)
		if !bytes.HasPrefix(b, []byte("age-encryption.org/v1\n")) {
			reply(400, map[string]string{"error": "Only encrypted sync files are accepted"})
			return
		}
		if f.files[email] == nil {
			f.files[email] = map[string]fakeSyncFile{}
		}
		f.version++
		f.puts++
		f.files[email][dev] = fakeSyncFile{b, f.version}
		reply(200, map[string]any{"version": f.version})
	case "DELETE sync":
		delete(f.files[email], dev)
		reply(200, map[string]bool{"ok": true})
	case "POST signout":
		for k, v := range f.tokens {
			if v == email && "Bearer "+k == r.Header.Get("Authorization") {
				delete(f.tokens, k)
			}
		}
		reply(200, map[string]bool{"ok": true})
	default:
		reply(404, map[string]string{"error": "Unknown request"})
	}
}

func syncSignIn(t *testing.T, a *App, api, email string) SyncResult {
	t.Helper()
	if r := a.SyncSignInStart(api, email); r.Error != "" {
		t.Fatal(r.Error)
	}
	r := a.SyncSignInFinish("123456")
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	return r
}

func mustCycle(t *testing.T, apps ...*App) {
	t.Helper()
	for _, a := range apps {
		if err := a.syncCycle(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSyncBetweenDevices(t *testing.T) {
	fake, api := newFakeSyncServer(t)
	a, b := newBackupApp(t), newBackupApp(t)

	// device A: some settings, then the first sign-in makes the sync code
	a.SetNickColor("bob", "#ff8800")
	a.store.Update(func(c *Config) {
		c.Settings.HighlightWords = []string{"tshirtgang"}
		c.Settings.FontSize = 16
		c.Settings.LogFolder = `C:\mylogs` // stays on this device
		c.Plugins.URLCommands = []URLCommand{{Trigger: "!solar", URL: "https://example.com/solar", Enabled: true, OnlyMe: true}}
	})
	a.store.SaveTheme(Theme{Name: "Night Owl", Base: "dark", Vars: map[string]string{"bg": "#101010"}})
	if r := syncSignIn(t, a, api, "andrew@example.com"); r.HasData {
		t.Fatal("empty account reported data")
	}
	start := a.SyncStart(nil)
	if start.Error != "" || start.Code == "" {
		t.Fatalf("start: %+v", start)
	}

	// device B: its own colour for bob (A's wins on joining) and one only it has
	b.SetNickColor("bob", "#000099")
	b.SetNickColor("carol", "#00aa00")
	if r := syncSignIn(t, b, api, "andrew@example.com"); !r.HasData {
		t.Fatal("account with data reported empty")
	}
	if r := b.SyncJoin("wrong-code-wrong-code-wrong-code-ab", nil); r.Error == "" {
		t.Fatal("joined with the wrong code")
	}
	if r := b.SyncJoin(strings.ToLower(strings.ReplaceAll(start.Code, "-", " ")), nil); r.Error != "" {
		t.Fatal(r.Error)
	}
	bc := b.store.Get()
	if bc.NickColors["bob"] != "#ff8800" || bc.NickColors["carol"] != "#00aa00" {
		t.Fatalf("after join: %v", bc.NickColors)
	}
	if bc.Settings.FontSize != 16 || len(bc.Settings.HighlightWords) != 1 || bc.Settings.LogFolder == `C:\mylogs` {
		t.Fatalf("settings after join: %+v", bc.Settings)
	}
	if len(bc.Plugins.URLCommands) != 1 || bc.Plugins.URLCommands[0].URL != "https://example.com/solar" {
		t.Fatalf("plugins after join: %+v", bc.Plugins.URLCommands)
	}
	if th := b.store.Themes(); len(th) != 1 || th[0].Vars["bg"] != "#101010" {
		t.Fatalf("themes after join: %+v", th)
	}
	mustCycle(t, a)
	if a.store.Get().NickColors["carol"] != "#00aa00" {
		t.Fatal("B's own colour didn't reach A")
	}

	// edits on both devices at once both survive
	time.Sleep(2 * time.Millisecond)
	a.store.Update(func(c *Config) { c.Settings.FontSize = 18 })
	b.store.Update(func(c *Config) { c.Shortcuts = append(c.Shortcuts, Shortcut{"$hi", "hello there"}) })
	mustCycle(t, a, b, a)
	for _, x := range []*App{a, b} {
		c := x.store.Get()
		found := false
		for _, s := range c.Shortcuts {
			found = found || s.Trigger == "$hi"
		}
		if c.Settings.FontSize != 18 || !found {
			t.Fatalf("merge: font %d, shortcut %v", c.Settings.FontSize, found)
		}
	}

	// a deletion spreads
	a.SetNickColor("carol", "")
	a.store.Update(func(c *Config) { c.Settings.HighlightWords = nil })
	mustCycle(t, a, b)
	if _, ok := b.store.Get().NickColors["carol"]; ok || len(b.store.Get().Settings.HighlightWords) != 0 {
		t.Fatalf("deletion didn't reach B: %v %v", b.store.Get().NickColors, b.store.Get().Settings.HighlightWords)
	}

	// a category turned off on B stops both ways
	cats := b.GetSyncStatus().Categories
	cats["nickColors"] = false
	b.SyncSetCategories(cats)
	a.SetNickColor("dave", "#123456")
	mustCycle(t, a, b)
	if _, ok := b.store.Get().NickColors["dave"]; ok {
		t.Fatal("a category turned off still synced")
	}

	// nothing changed: nothing uploaded
	before := fake.puts
	mustCycle(t, a, b, a, b)
	if fake.puts != before {
		t.Fatalf("idle syncs uploaded %d times", fake.puts-before)
	}

	// status shows both devices
	if st := a.GetSyncStatus(); len(st.Devices) != 2 || !st.Enabled || st.Email != "andrew@example.com" {
		t.Fatalf("status %+v", st)
	}

	// stopping removes this device's file and signs out
	b.SyncStop()
	if st := b.GetSyncStatus(); st.Enabled || st.SignedIn {
		t.Fatalf("after stop: %+v", st)
	}
	mustCycle(t, a)
	if st := a.GetSyncStatus(); len(st.Devices) != 1 {
		t.Fatalf("devices after B stopped: %+v", st.Devices)
	}
}

// Logins and passwords never leave the device, even with servers synced.
func TestSyncNeverSendsSecrets(t *testing.T) {
	fake, api := newFakeSyncServer(t)
	a, b := newBackupApp(t), newBackupApp(t)
	secrets := []string{"srv-PASSWORD", "sasl-USER", "sasl-PASSWORD", "bnc-USER", "bnc-PASSWORD", "proxy-PASSWORD", "proxy-USER",
		"AGE-SECRET-KEY-1TESTTEST", "chankey123", "IDENTIFY nspass", "ClientKeyPEM", "imgur-client-id"}
	sv := a.SaveServer(ServerConfig{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "andrew",
		Password: secrets[0], AuthMethod: "sasl", AuthUser: secrets[1], AuthPass: secrets[2], Bouncer: "soju", BouncerUser: secrets[3],
		BouncerPass: secrets[4], ProxyType: "socks5", ProxyHost: "127.0.0.1", ProxyPort: 9050, ProxyPass: secrets[5], ProxyUser: secrets[6],
		EncryptionKey: secrets[7], AutoJoin: []string{"#linux", "#secret " + secrets[8]}, Perform: []string{"/msg NickServ " + secrets[9]}, ClientKey: secrets[10]})
	a.store.Update(func(c *Config) { c.Settings.ImgurClientID = secrets[11] })
	a.SetChannelNotify(sv.ID, "#linux", "all", 0)
	syncSignIn(t, a, api, "andrew@example.com")
	cats := cleanCats(nil)
	cats["servers"] = true
	start := a.SyncStart(cats)
	if start.Error != "" {
		t.Fatal(start.Error)
	}
	for _, f := range fake.files["andrew@example.com"] {
		p, err := openSync(f.data, start.Code)
		if err != nil {
			t.Fatal(err)
		}
		plain := string(mustJSON(p))
		for _, s := range secrets {
			if strings.Contains(plain, s) {
				t.Errorf("synced file contains %q", s)
			}
		}
		if !strings.Contains(plain, "irc.libera.chat") {
			t.Error("server wasn't synced at all")
		}
		if bytes.Contains(f.data, []byte("libera")) {
			t.Error("the server could read the file")
		}
	}

	// the other device gets the server without logins, adds its own, and keeps them
	syncSignIn(t, b, api, "andrew@example.com")
	if r := b.SyncJoin(start.Code, cats); r.Error != "" {
		t.Fatal(r.Error)
	}
	bs := b.store.Get().Servers
	if len(bs) != 1 || bs[0].Host != "irc.libera.chat" || bs[0].AuthPass != "" || bs[0].BouncerUser != "" || len(bs[0].Perform) != 0 {
		t.Fatalf("server on B: %+v", bs)
	}
	if bs[0].AutoJoin[1] != "#secret" {
		t.Fatalf("channel key synced: %v", bs[0].AutoJoin)
	}
	if b.store.Get().ChannelNotify[bs[0].ID+"/#linux"].Level != "all" {
		t.Fatal("per-channel notification setting didn't follow the server")
	}
	s := bs[0]
	s.AuthMethod, s.AuthUser, s.AuthPass = "sasl", "b-user", "b-pass"
	b.SaveServer(s)
	time.Sleep(2 * time.Millisecond)
	a.store.Update(func(c *Config) { c.Servers[0].Nick = "andrew2" })
	mustCycle(t, a, b)
	got := b.store.Get().Servers[0]
	if got.Nick != "andrew2" || got.AuthPass != "b-pass" || got.AuthUser != "b-user" {
		t.Fatalf("B lost its own login or missed the nick: %+v", got)
	}
}

func TestSyncServerAddress(t *testing.T) {
	for in, want := range map[string]string{
		"": "",
		"https://irchatterbox.com/account/api.php": "",
		"irchatterbox.com":                         "",
		"sync.example.net":                         "https://sync.example.net/account/api.php",
		"https://example.net/irc/api.php":          "https://example.net/irc/api.php",
		"http://127.0.0.1:8781":                    "http://127.0.0.1:8781/account/api.php",
	} {
		got, err := cleanSyncServer(in)
		if err != nil || got != want {
			t.Errorf("%q → %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, bad := range []string{"http://example.net", "ftp://x.y", "https://"} {
		if _, err := cleanSyncServer(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
