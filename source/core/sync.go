package core

// Sync: settings shared between your devices, chosen by category (appearance, nick colours,
// highlights, plugins…). Logins and passwords are never synced.
//
// Each device uploads its own file to the sync server (an irchatterbox.com account, or a
// server of your own running the same account site) and reads everyone else's. A file is
// encrypted on the device with age, using the sync code as the passphrase, so the server
// only ever holds files it can't read; it refuses anything that isn't encrypted.
//
// Inside, every setting is a separate item ("nickColors/bob", "appearance/set/fontSize",
// "plugins/url/!solar"…) with the time it last changed and the device that changed it.
// Merging keeps the newest version of each item, so changing a colour on one device and a
// theme on another keeps both. Deleting leaves a tombstone so the deletion spreads too.

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"
)

// SyncConfig is this device's sync setup (kept in config.json, never in backups).
type SyncConfig struct {
	Enabled    bool            `json:"enabled"`
	Server     string          `json:"server,omitempty"` // the sync server's api.php; "" = irchatterbox.com
	Email      string          `json:"email,omitempty"`  // the account signed in on this device
	Categories map[string]bool `json:"categories,omitempty"`
	DeviceID   string          `json:"deviceId,omitempty"`
	// only when there's no credential store (some Linux setups, Android):
	Key   string `json:"key,omitempty"`   // the sync code
	Token string `json:"token,omitempty"` // the account sign-in for this device
}

// DefaultSyncServer is the account site's API for the apps.
const DefaultSyncServer = "https://irchatterbox.com/account/api.php"

func (sc SyncConfig) serverURL() string {
	if sc.Server == "" {
		return DefaultSyncServer
	}
	return sc.Server
}

// SyncCategory is one choice in Settings → Sync.
type SyncCategory struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Default bool   `json:"default"`
}

var syncCategories = []SyncCategory{
	{"appearance", "Appearance", "Theme, custom themes, font, layout, time format, language", true},
	{"nickColors", "Nick colours", "Colours you've set for people", true},
	{"highlights", "Highlights and ignores", "Highlight words, hidden words, ignore list, friends list", true},
	{"notifications", "Notifications and sounds", "What notifies you and which built-in sound plays", true},
	{"chat", "Chat behaviour", "Inline media, link previews, joins and quits, quit message, auto-away", true},
	{"shortcuts", "Shortcuts", "Text shortcuts like $shrug", true},
	{"plugins", "Plugin settings", "Bot prefix, temperature converter, now playing, URL commands", true},
	{"servers", "Servers and channels", "Network addresses, nicks, auto-join channels, favourites and per-channel settings. Logins and passwords are never synced: enter them on each device.", false},
}

// Settings fields synced per category. Anything not listed (folders, startup, tray,
// updates, upload host, DCC, keep-alive…) belongs to one device and stays put.
var syncSettingFields = map[string][]string{
	"appearance":    {"theme", "lightTheme", "darkTheme", "fontSize", "fontFamily", "compactMode", "chatLayout", "timeFormat", "showSeconds", "markerLine", "nickContrast", "showUserList", "language"},
	"notifications": {"notifications", "notifyPMs", "notifyFriends", "flashOnPing", "soundHighlight", "soundPM", "soundVolume"},
	"chat": {"inlineImages", "inlineYouTube", "inlineVideos", "linkPreviews", "showJoinsParts", "mentionPopup", "spellCheck", "sendTyping",
		"pasteWarnLines", "confirmModeration", "kickReason", "quitMessage", "autoAway", "autoAwayMinutes", "autoAwayMessage", "confirmOnClose", "historyLines"},
}

// Settings lists synced item by item (an added word and a removed word on two devices both stick).
var syncSettingSets = map[string]string{"hl": "highlightWords", "filter": "filterWords", "ignore": "ignore", "friend": "notifyList"}

var syncPluginFields = []string{"botPrefix", "weatherEnabled", "weatherChannels", "weatherCooldown", "weatherFromMe", "weatherFromOthers"}

// syncServer is what's synced of a server: where it is and how you appear there. No
// passwords, no login names, no auth method, no proxy, no certificates, no Perform lines
// (they often hold "/msg NickServ IDENTIFY …").
type syncServer struct {
	Name           string   `json:"name"`
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	TLS            bool     `json:"tls"`
	Nick           string   `json:"nick"`
	AltNick        string   `json:"altNick"`
	Username       string   `json:"username"`
	Realname       string   `json:"realname"`
	AutoConnect    bool     `json:"autoConnect"`
	AutoJoin       []string `json:"autoJoin"`
	RememberChans  bool     `json:"rememberChannels"`
	Encoding       string   `json:"encoding"`
	ChannelOrder   []string `json:"channelOrder"`
	Bouncer        string   `json:"bouncer"`
	BouncerNetwork string   `json:"bouncerNetwork"`
	IconText       string   `json:"iconText"`
	LetterIcon     bool     `json:"letterIcon"`
	JoinsParts     string   `json:"joinsParts"`
}

func toSyncServer(s ServerConfig) syncServer {
	aj := make([]string, 0, len(s.AutoJoin))
	for _, a := range s.AutoJoin {
		aj = append(aj, strings.Fields(a + " ")[0]) // channel keys are passwords: not synced
	}
	return syncServer{s.Name, s.Host, s.Port, s.TLS, s.Nick, s.AltNick, s.Username, s.Realname, s.AutoConnect, aj, s.RememberChans,
		s.Encoding, s.ChannelOrder, s.Bouncer, s.BouncerNetwork, s.IconText, s.LetterIcon, s.JoinsParts}
}

func (ss syncServer) applyTo(s *ServerConfig) {
	keys := map[string]string{} // keep this device's channel keys
	for _, a := range s.AutoJoin {
		if f := strings.Fields(a); len(f) > 1 {
			keys[lower(f[0])] = f[1]
		}
	}
	s.Name, s.Host, s.Port, s.TLS = ss.Name, ss.Host, ss.Port, ss.TLS
	s.Nick, s.AltNick, s.Username, s.Realname = ss.Nick, ss.AltNick, ss.Username, ss.Realname
	s.AutoConnect, s.RememberChans, s.Encoding, s.ChannelOrder = ss.AutoConnect, ss.RememberChans, ss.Encoding, ss.ChannelOrder
	s.Bouncer, s.BouncerNetwork = ss.Bouncer, ss.BouncerNetwork
	s.IconText, s.LetterIcon, s.JoinsParts = cleanIconText(ss.IconText), ss.LetterIcon, ss.JoinsParts
	s.AutoJoin = []string{}
	for _, ch := range ss.AutoJoin {
		if k := keys[lower(ch)]; k != "" {
			ch += " " + k
		}
		s.AutoJoin = append(s.AutoJoin, ch)
	}
	if s.ChannelOrder == nil {
		s.ChannelOrder = []string{}
	}
}

// ---------- the file each device writes ----------

type syncItem struct {
	V   json.RawMessage `json:"v,omitempty"`
	T   int64           `json:"t"`             // when it last changed (Unix ms; 0 = "anyone else's version wins")
	D   string          `json:"d"`             // device that changed it
	Del bool            `json:"del,omitempty"` // deleted
}

type syncPayload struct {
	Format  int                 `json:"format"`
	Device  syncDevice          `json:"device"`
	Items   map[string]syncItem `json:"items"`
	Written int64               `json:"written"`
}

type syncDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SyncDevice is a device seen in the sync location, for the status line.
type SyncDevice struct {
	Name    string `json:"name"`
	Updated int64  `json:"updated"`
	This    bool   `json:"this"`
}

// ---------- local state (sync-state.json, next to config.json) ----------

type syncStateItem struct {
	syncItem
	H string `json:"h,omitempty"` // hash of this device's own copy when last compared ("" = this device doesn't have it)
}

type syncState struct {
	Items     map[string]syncStateItem `json:"items"`
	Servers   map[string]string        `json:"servers"`  // local server ID → the ID the synced copy uses
	JoinCats  map[string]bool          `json:"joinCats"` // categories just turned on: others' versions win once
	LastSync  int64                    `json:"lastSync"`
	LastError string                   `json:"lastError"`
	Devices   []SyncDevice             `json:"devices"`
	Cursor    string                   `json:"cursor"`       // the server's version when we last fetched
	Known     map[string]SyncDevice    `json:"knownDevices"` // device ID → name and last write, from their files
}

func (a *App) syncStatePath() string { return filepath.Join(a.store.dir, "sync-state.json") }

func (a *App) loadSyncState() *syncState {
	st := &syncState{}
	if b, err := os.ReadFile(a.syncStatePath()); err == nil {
		json.Unmarshal(b, st)
	}
	if st.Items == nil {
		st.Items = map[string]syncStateItem{}
	}
	if st.Servers == nil {
		st.Servers = map[string]string{}
	}
	if st.JoinCats == nil {
		st.JoinCats = map[string]bool{}
	}
	if st.Known == nil {
		st.Known = map[string]SyncDevice{}
	}
	return st
}

func (a *App) saveSyncState(st *syncState) {
	b, _ := json.Marshal(st)
	tmp := a.syncStatePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, a.syncStatePath())
	}
}

// ---------- the sync code ----------

// newSyncCode makes a 160-bit code shown as 8 groups of 4: ABCD-EFGH-…
func newSyncCode() string {
	b := make([]byte, 20)
	rand.Read(b)
	return formatSyncCode(base32.StdEncoding.EncodeToString(b))
}

// normSyncCode accepts a code typed with or without dashes, spaces or lower case.
func normSyncCode(s string) (string, bool) {
	var out []byte
	for _, r := range strings.ToUpper(s) {
		if r >= 'A' && r <= 'Z' || r >= '2' && r <= '7' {
			out = append(out, byte(r))
		}
	}
	return string(out), len(out) == 32
}

func formatSyncCode(s string) string {
	s, _ = normSyncCode(s)
	var parts []string
	for i := 0; i < len(s); i += 4 {
		parts = append(parts, s[i:min(i+4, len(s))])
	}
	return strings.Join(parts, "-")
}

const (
	syncKeySecret   = "_sync/code"
	syncTokenSecret = "_sync/token"
)

// syncSecret reads the sync code or account token from the credential store, or from
// config.json where there's none.
func (a *App) syncSecret(name string) string {
	secretsMu.Lock()
	var v string
	if secretStoreUsable() {
		v, _ = secrets.Get(name)
	}
	secretsMu.Unlock()
	if v == "" {
		sc := a.store.Get().Sync
		if name == syncKeySecret {
			v = sc.Key
		} else {
			v = sc.Token
		}
	}
	return v
}

func (a *App) setSyncSecret(name, value string) {
	secretsMu.Lock()
	stored := false
	if secretStoreUsable() {
		if value == "" {
			secrets.Delete(name)
			stored = true
		} else {
			stored = secrets.Set(name, value) == nil
		}
	}
	secretsMu.Unlock()
	keep := ""
	if !stored {
		keep = value
	}
	a.store.Update(func(c *Config) {
		if name == syncKeySecret {
			c.Sync.Key = keep
		} else {
			c.Sync.Token = keep
		}
	})
}

func (a *App) syncCode() string          { return a.syncSecret(syncKeySecret) }
func (a *App) setSyncCode(code string)   { a.setSyncSecret(syncKeySecret, code) }
func (a *App) syncToken() string         { return a.syncSecret(syncTokenSecret) }
func (a *App) setSyncToken(token string) { a.setSyncSecret(syncTokenSecret, token) }

// sealSync encrypts a payload with the sync code. The code is long and random, so a low
// scrypt cost is plenty and keeps phones quick.
func sealSync(plain []byte, code string) ([]byte, error) {
	r, err := age.NewScryptRecipient(code)
	if err != nil {
		return nil, err
	}
	r.SetWorkFactor(12)
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, r)
	if err != nil {
		return nil, err
	}
	w.Write(plain)
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func openSync(data []byte, code string) (*syncPayload, error) {
	id, err := age.NewScryptIdentity(code)
	if err != nil {
		return nil, err
	}
	id.SetMaxWorkFactor(18)
	r, err := age.Decrypt(bytes.NewReader(data), id)
	if err != nil {
		return nil, errWrongSyncCode
	}
	b, err := io.ReadAll(io.LimitReader(r, 8<<20))
	if err != nil {
		return nil, err
	}
	var p syncPayload
	if err := json.Unmarshal(b, &p); err != nil || p.Format != 1 {
		return nil, errors.New("a sync file is from a newer version of IRChatterbox or is damaged")
	}
	return &p, nil
}

var errWrongSyncCode = errors.New("the sync code doesn't match")

// ---------- where the files are kept ----------

type syncBlob struct {
	Device string
	Data   []byte
}

// syncFetch is what a fetch returns: the files changed since the cursor, every device that
// has a file, and the cursor for next time.
type syncFetch struct {
	Blobs   []syncBlob
	Devices []string
	Cursor  string
}

// syncTransport is where the files are kept. Today that's the account server; Google
// Drive, OneDrive or Dropbox could be added later behind the same interface.
type syncTransport interface {
	Fetch(cursor string) (syncFetch, error)
	Put(device string, data []byte) error
	Remove(device string) error
}

var errSyncSignedOut = errors.New("signed out of the sync account: sign in again in Settings → Sync")

// accountTransport talks to the account site's api.php.
type accountTransport struct {
	api   string
	token string
}

var syncHTTP = &http.Client{Timeout: 25 * time.Second, CheckRedirect: httpsOnlyRedirect}

// cleanSyncServer checks a sync server address: https (or plain http to this computer, for
// testing), and the API path added when only the site was given.
func cleanSyncServer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("that isn't a web address")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", errors.New("the sync server must use https://")
	}
	if !strings.HasSuffix(u.Path, ".php") {
		u.Path = strings.TrimRight(u.Path, "/") + "/account/api.php"
	}
	u.RawQuery, u.Fragment = "", ""
	out := u.String()
	if out == DefaultSyncServer {
		return "", nil
	}
	return out, nil
}

// syncCall makes one request to the sync server. in is sent as JSON (or raw bytes for a
// []byte); the JSON reply is decoded into out.
func syncCall(api, method, op string, query url.Values, token string, in any, out any) error {
	if query == nil {
		query = url.Values{}
	}
	query.Set("op", op)
	var body io.Reader
	ctype := "application/json"
	switch v := in.(type) {
	case nil:
	case []byte:
		body, ctype = bytes.NewReader(v), "application/octet-stream"
	default:
		body = bytes.NewReader(mustJSON(v))
	}
	req, err := http.NewRequest(method, api+"?"+query.Encode(), body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "IRChatterbox/"+appVersion)
	if body != nil {
		req.Header.Set("Content-Type", ctype)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := syncHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach the sync server: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	var e struct {
		Error string `json:"error"`
	}
	json.Unmarshal(b, &e)
	if resp.StatusCode == 401 && token != "" {
		return errSyncSignedOut
	}
	if resp.StatusCode != 200 {
		if e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("the sync server answered %s", resp.Status)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return errors.New("the sync server's reply wasn't understood (is the address right?)")
		}
	}
	return nil
}

func (t accountTransport) Fetch(cursor string) (syncFetch, error) {
	var r struct {
		Version int64 `json:"version"`
		Devices []struct {
			Device string `json:"device"`
		} `json:"devices"`
		Files []struct {
			Device string `json:"device"`
			Data   string `json:"data"`
		} `json:"files"`
	}
	q := url.Values{}
	if cursor != "" {
		q.Set("since", cursor)
	}
	if err := syncCall(t.api, "GET", "sync", q, t.token, nil, &r); err != nil {
		return syncFetch{}, err
	}
	f := syncFetch{Cursor: strconv.FormatInt(r.Version, 10)}
	for _, d := range r.Devices {
		f.Devices = append(f.Devices, d.Device)
	}
	for _, x := range r.Files {
		if b, err := base64.StdEncoding.DecodeString(x.Data); err == nil {
			f.Blobs = append(f.Blobs, syncBlob{Device: x.Device, Data: b})
		}
	}
	return f, nil
}

func (t accountTransport) Put(device string, data []byte) error {
	return syncCall(t.api, "PUT", "sync", url.Values{"device": {device}}, t.token, data, nil)
}

func (t accountTransport) Remove(device string) error {
	return syncCall(t.api, "DELETE", "sync", url.Values{"device": {device}}, t.token, nil, nil)
}

func (a *App) syncTransport(sc SyncConfig) (syncTransport, error) {
	tok := a.syncToken()
	if tok == "" {
		return nil, errSyncSignedOut
	}
	return accountTransport{api: sc.serverURL(), token: tok}, nil
}

// ---------- reading this device's settings as items ----------

func syncCatOf(key string) string { c, _, _ := strings.Cut(key, "/"); return c }

func (sc SyncConfig) catOn(cat string) bool {
	if v, ok := sc.Categories[cat]; ok {
		return v
	}
	for _, c := range syncCategories {
		if c.ID == cat {
			return c.Default
		}
	}
	return false
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func fieldsOf(v any) map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	json.Unmarshal(mustJSON(v), &m)
	return m
}

// syncItemsOf lists this device's synced settings. st.Servers gets an entry for any server
// not yet known to sync.
func syncItemsOf(cfg Config, themes []Theme, st *syncState) map[string]json.RawMessage {
	sc := cfg.Sync
	out := map[string]json.RawMessage{}
	set := fieldsOf(cfg.Settings)
	for cat, fields := range syncSettingFields {
		if !sc.catOn(cat) {
			continue
		}
		for _, f := range fields {
			v, ok := set[f]
			if !ok {
				continue
			}
			if (f == "soundHighlight" || f == "soundPM") && bytes.HasPrefix(v, []byte(`"file:`)) {
				continue // a sound file on this computer: nothing to share
			}
			out[cat+"/set/"+f] = v
		}
	}
	if sc.catOn("appearance") {
		for _, t := range themes {
			if strings.TrimSpace(t.Name) != "" {
				out["appearance/theme/"+t.Name] = mustJSON(t)
			}
		}
	}
	if sc.catOn("highlights") {
		for short, f := range syncSettingSets {
			var list []string
			json.Unmarshal(set[f], &list)
			for _, w := range list {
				if w = strings.TrimSpace(w); w != "" {
					out["highlights/"+short+"/"+w] = json.RawMessage("true")
				}
			}
		}
	}
	if sc.catOn("nickColors") {
		for n, c := range cfg.NickColors {
			if validColor(c) {
				out["nickColors/"+n] = mustJSON(c)
			}
		}
	}
	if sc.catOn("shortcuts") {
		for _, s := range cfg.Shortcuts {
			if s.Trigger != "" {
				out["shortcuts/"+s.Trigger] = mustJSON(s)
			}
		}
	}
	if sc.catOn("plugins") {
		pf := fieldsOf(cfg.Plugins)
		for _, f := range syncPluginFields {
			if v, ok := pf[f]; ok {
				out["plugins/set/"+f] = v
			}
		}
		m := cfg.Plugins.Music
		m.File = "" // a file on this computer
		out["plugins/music"] = mustJSON(m)
		for _, u := range cfg.Plugins.URLCommands {
			if u.Trigger != "" {
				out["plugins/url/"+lower(u.Trigger)] = mustJSON(u)
			}
		}
	}
	if sc.catOn("servers") {
		for _, s := range cfg.Servers {
			sk := st.Servers[s.ID]
			if sk == "" {
				sk = s.ID
				st.Servers[s.ID] = sk
			}
			out["servers/"+sk] = mustJSON(toSyncServer(s))
		}
		local := map[string]string{} // local ID → sync ID
		for id, sk := range st.Servers {
			local[id] = sk
		}
		now := time.Now().UnixMilli()
		for k, r := range cfg.ChannelNotify {
			id, buf, _ := strings.Cut(k, "/")
			if sk := local[id]; sk != "" && !(r.Until > 0 && r.Until <= now) {
				out["servers/"+sk+"/notify/"+buf] = mustJSON(r)
			}
		}
		for k, v := range cfg.ChannelJoins {
			id, buf, _ := strings.Cut(k, "/")
			if sk := local[id]; sk != "" {
				out["servers/"+sk+"/joins/"+buf] = mustJSON(v)
			}
		}
		for _, f := range cfg.Favourites {
			if sk := local[f.Server]; sk != "" {
				out["servers/"+sk+"/fav/"+lower(f.Buffer)] = mustJSON(f.Buffer)
			}
		}
	}
	return out
}

func hashOf(v json.RawMessage) string {
	if v == nil {
		return ""
	}
	h := sha256.Sum256(v)
	return hex.EncodeToString(h[:8])
}

// newer: should remote item r replace what we have? The newest change wins; equal times
// are settled by device ID so every device picks the same one.
func newer(r syncItem, cur syncStateItem) bool {
	if r.T != cur.T {
		return r.T > cur.T
	}
	if r.D == cur.D {
		return false
	}
	return r.D > cur.D && (r.Del != cur.Del || string(r.V) != string(cur.V))
}

// ---------- the sync itself ----------

// syncCycle reads every device's file, merges, applies what changed here, and writes this
// device's file. Only one runs at a time.
func (a *App) syncCycle() error {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	cfg := a.store.Get()
	sc := cfg.Sync
	if !sc.Enabled {
		return nil
	}
	st := a.loadSyncState()
	err := a.syncOnce(cfg, st)
	st.LastError = ""
	if err != nil {
		st.LastError = err.Error()
		log.Printf("sync: %v", err)
	} else {
		st.LastSync = time.Now().UnixMilli()
	}
	a.saveSyncState(st)
	a.emitUI("syncStatus", a.GetSyncStatus())
	return err
}

func (a *App) syncOnce(cfg Config, st *syncState) error {
	sc := cfg.Sync
	code := a.syncCode()
	if code == "" {
		return errors.New("this device has no sync code: set sync up again")
	}
	tr, err := a.syncTransport(sc)
	if err != nil {
		return err
	}
	dev := sc.DeviceID
	now := time.Now().UnixMilli()

	// 1. changes made on this device since last time
	local := syncItemsOf(cfg, a.store.Themes(), st)
	dirty := false
	for k, v := range local {
		h := hashOf(v)
		cur, ok := st.Items[k]
		if ok && cur.H == h && !cur.Del {
			continue
		}
		t := now
		if st.JoinCats[syncCatOf(k)] {
			t = 0 // a category just turned on: what the other devices have wins
		}
		st.Items[k] = syncStateItem{syncItem: syncItem{V: v, T: t, D: dev}, H: h}
		dirty = true
	}
	for k, cur := range st.Items {
		if !sc.catOn(syncCatOf(k)) || cur.Del || cur.H == "" {
			continue
		}
		if _, ok := local[k]; !ok {
			st.Items[k] = syncStateItem{syncItem: syncItem{T: now, D: dev, Del: true}}
			dirty = true
		}
	}

	// 2. other devices' files changed since last time
	fetched, err := tr.Fetch(st.Cursor)
	if err != nil {
		return err
	}
	ownFound, unreadable := false, 0
	present := map[string]bool{}
	for _, d := range fetched.Devices {
		present[d] = true
		ownFound = ownFound || d == dev
	}
	changed := map[string]bool{}
	for _, b := range fetched.Blobs {
		p, err := openSync(b.Data, code)
		if err != nil {
			unreadable++
			continue
		}
		st.Known[b.Device] = SyncDevice{Name: p.Device.Name, Updated: p.Written}
		for k, r := range p.Items {
			if !sc.catOn(syncCatOf(k)) {
				continue
			}
			cur, ok := st.Items[k]
			if ok && !newer(r, cur) {
				continue
			}
			if !ok && r.Del {
				// a deletion of something this device never had
				st.Items[k] = syncStateItem{syncItem: r}
				continue
			}
			st.Items[k] = syncStateItem{syncItem: r, H: cur.H}
			changed[k] = true
			dirty = true
		}
	}
	st.JoinCats = map[string]bool{}

	// 3. apply what others changed
	if len(changed) > 0 {
		a.applySyncItems(st, changed)
		after := syncItemsOf(a.store.Get(), a.store.Themes(), st)
		for k := range changed {
			it := st.Items[k]
			it.H = hashOf(after[k]) // "" when this device doesn't have it (or couldn't take it)
			st.Items[k] = it
		}
		a.emitUI("syncApplied", map[string]any{"items": len(changed)})
	}

	// 4. write this device's file
	if dirty || !ownFound {
		p := syncPayload{Format: 1, Device: syncDevice{ID: dev, Name: syncDeviceName()}, Items: map[string]syncItem{}, Written: now}
		cutoff := now - 180*24*3600*1000 // forget deletions after six months
		for k, it := range st.Items {
			if !sc.catOn(syncCatOf(k)) || (it.Del && it.T < cutoff) {
				continue
			}
			p.Items[k] = it.syncItem
		}
		plain, _ := json.Marshal(p)
		data, err := sealSync(plain, code)
		if err != nil {
			return err
		}
		if err := tr.Put(dev, data); err != nil {
			return fmt.Errorf("couldn't upload this device's settings: %w", err)
		}
		st.Known[dev] = SyncDevice{Name: p.Device.Name, Updated: now}
		present[dev] = true
	}
	st.Cursor = fetched.Cursor
	st.Devices = nil
	for id := range present {
		d, ok := st.Known[id]
		if !ok {
			d.Name = "Another device"
		}
		d.This = id == dev
		st.Devices = append(st.Devices, d)
	}
	for id := range st.Known {
		if !present[id] {
			delete(st.Known, id)
		}
	}
	sort.Slice(st.Devices, func(i, j int) bool { return st.Devices[i].Updated > st.Devices[j].Updated })
	if unreadable > 0 {
		return fmt.Errorf("%d device(s) on this account use a different sync code and were skipped", unreadable)
	}
	return nil
}

func syncDeviceName() string {
	n, _ := os.Hostname()
	if n == "" || n == "localhost" {
		return "Android"
	}
	return n
}

// applySyncItems writes other devices' changes into this device's settings.
func (a *App) applySyncItems(st *syncState, changed map[string]bool) {
	keys := make([]string, 0, len(changed))
	for k := range changed {
		keys = append(keys, k)
	}
	sort.Strings(keys) // servers/<id> before servers/<id>/…
	var saveThemes []Theme
	var dropThemes, dropServers []string
	updatedServers := map[string]ServerConfig{}

	a.store.Update(func(c *Config) {
		set := fieldsOf(c.Settings)
		plug := fieldsOf(c.Plugins)
		setChanged, plugChanged := false, false
		localID := func(sk string) string {
			for id, s := range st.Servers {
				if s == sk {
					return id
				}
			}
			return ""
		}
		for _, k := range keys {
			it := st.Items[k]
			parts := strings.SplitN(k, "/", 4)
			cat := parts[0]
			switch {
			case len(parts) == 3 && parts[1] == "set" && cat != "plugins":
				if it.Del || bytes.HasPrefix(it.V, []byte(`"file:`)) {
					continue
				}
				set[parts[2]] = it.V
				setChanged = true
			case cat == "appearance" && len(parts) >= 3 && parts[1] == "theme":
				name := strings.Join(parts[2:], "/")
				if it.Del {
					dropThemes = append(dropThemes, name)
				} else {
					var t Theme
					if json.Unmarshal(it.V, &t) == nil && t.Name == name {
						saveThemes = append(saveThemes, t)
					}
				}
			case cat == "highlights" && len(parts) >= 3:
				f := syncSettingSets[parts[1]]
				if f == "" {
					continue
				}
				word := strings.Join(parts[2:], "/")
				var list []string
				json.Unmarshal(set[f], &list)
				list = removeStr(list, word)
				if !it.Del {
					list = append(list, word)
				}
				set[f] = mustJSON(list)
				setChanged = true
			case cat == "nickColors":
				nick := strings.TrimPrefix(k, "nickColors/")
				if c.NickColors == nil {
					c.NickColors = map[string]string{}
				}
				var col string
				json.Unmarshal(it.V, &col)
				if it.Del {
					delete(c.NickColors, nick)
				} else if validColor(col) {
					c.NickColors[nick] = col
				}
			case cat == "shortcuts":
				trig := strings.TrimPrefix(k, "shortcuts/")
				var keep []Shortcut
				for _, s := range c.Shortcuts {
					if s.Trigger != trig {
						keep = append(keep, s)
					}
				}
				var s Shortcut
				if !it.Del && json.Unmarshal(it.V, &s) == nil && s.Trigger == trig {
					keep = append(keep, s)
				}
				c.Shortcuts = keep
			case cat == "plugins" && len(parts) == 3 && parts[1] == "set":
				if !it.Del {
					plug[parts[2]] = it.V
					plugChanged = true
				}
			case k == "plugins/music":
				var m MusicSettings
				if !it.Del && json.Unmarshal(it.V, &m) == nil {
					m.File = c.Plugins.Music.File
					plug["music"] = mustJSON(m)
					plugChanged = true
				}
			case cat == "plugins" && len(parts) >= 3 && parts[1] == "url":
				trig := strings.Join(parts[2:], "/")
				var list []URLCommand
				json.Unmarshal(plug["urlCommands"], &list)
				var keep []URLCommand
				replaced := false
				var u URLCommand
				valid := !it.Del && json.Unmarshal(it.V, &u) == nil && lower(u.Trigger) == trig
				for _, x := range list {
					if lower(x.Trigger) == trig {
						if valid && !replaced {
							keep = append(keep, u)
							replaced = true
						}
						continue
					}
					keep = append(keep, x)
				}
				if valid && !replaced {
					keep = append(keep, u)
				}
				plug["urlCommands"] = mustJSON(keep)
				plugChanged = true
			case cat == "servers" && len(parts) == 2:
				sk := parts[1]
				id := localID(sk)
				if it.Del {
					if id != "" {
						dropServers = append(dropServers, id)
						delete(st.Servers, id)
					}
					continue
				}
				var ss syncServer
				if json.Unmarshal(it.V, &ss) != nil || ss.Host == "" {
					continue
				}
				if id == "" {
					// the same network added separately on this device? use that one
					for _, s := range c.Servers {
						if _, mapped := st.Servers[s.ID]; !mapped && strings.EqualFold(s.Host, ss.Host) && s.Port == ss.Port &&
							strings.EqualFold(s.BouncerNetwork, ss.BouncerNetwork) {
							id = s.ID
							break
						}
					}
				}
				if id == "" {
					id = newID()
					c.Servers = append(c.Servers, ServerConfig{ID: id, AuthMethod: "none", Perform: []string{}, AutoJoin: []string{}})
				}
				st.Servers[id] = sk
				for i := range c.Servers {
					if c.Servers[i].ID == id {
						ss.applyTo(&c.Servers[i])
						updatedServers[id] = c.Servers[i]
					}
				}
			case cat == "servers" && len(parts) == 4:
				id := localID(parts[1])
				if id == "" {
					continue
				}
				buf := parts[3]
				nk := notifyKey(id, buf)
				switch parts[2] {
				case "notify":
					var r NotifyRule
					if c.ChannelNotify == nil {
						c.ChannelNotify = map[string]NotifyRule{}
					}
					if it.Del || json.Unmarshal(it.V, &r) != nil {
						delete(c.ChannelNotify, nk)
					} else {
						c.ChannelNotify[nk] = r
					}
				case "joins":
					var v string
					if c.ChannelJoins == nil {
						c.ChannelJoins = map[string]string{}
					}
					if it.Del || json.Unmarshal(it.V, &v) != nil || (v != "show" && v != "hide") {
						delete(c.ChannelJoins, nk)
					} else {
						c.ChannelJoins[nk] = v
					}
				case "fav":
					var name string
					json.Unmarshal(it.V, &name)
					var keep []Favourite
					for _, f := range c.Favourites {
						if !(f.Server == id && lower(f.Buffer) == buf) {
							keep = append(keep, f)
						}
					}
					if !it.Del && name != "" && lower(name) == buf {
						keep = append(keep, Favourite{Server: id, Buffer: name})
					}
					c.Favourites = keep
				}
			}
		}
		if setChanged {
			var s Settings
			if json.Unmarshal(mustJSON(set), &s) == nil {
				c.Settings = s
			}
		}
		if plugChanged {
			var p PluginSettings
			if json.Unmarshal(mustJSON(plug), &p) == nil {
				p.ScriptsEnabled = c.Plugins.ScriptsEnabled // on or off per device
				c.Plugins = p
			}
		}
	})
	for _, t := range saveThemes {
		a.store.SaveTheme(t)
	}
	for _, n := range dropThemes {
		a.store.DeleteTheme(n)
	}
	a.mu.Lock()
	for id, s := range updatedServers {
		if c, ok := a.conns[id]; ok {
			c.cfg = s
		}
	}
	a.mu.Unlock()
	for _, id := range dropServers {
		a.DeleteServer(id)
	}
}

func removeStr(list []string, s string) []string {
	out := list[:0:0]
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// ---------- running in the background ----------

// startSync syncs shortly after launch, a few seconds after every settings change (so a
// burst of changes is one upload), every 10 minutes, and when the window comes back into
// focus (SyncPoll, at most once a minute).
func (a *App) startSync() {
	a.store.setOnChange(a.syncSoon)
	go func() {
		time.Sleep(3 * time.Second)
		a.syncCycle()
		for range time.Tick(10 * time.Minute) {
			if a.store.Get().Sync.Enabled {
				a.syncCycle()
			}
		}
	}()
}

func (a *App) syncSoon() {
	if !a.store.Get().Sync.Enabled {
		return
	}
	a.syncTimerMu.Lock()
	defer a.syncTimerMu.Unlock()
	if a.syncTimer != nil {
		a.syncTimer.Stop()
	}
	a.syncTimer = time.AfterFunc(3*time.Second, func() { a.syncCycle() })
}

// SyncPoll is called by the UI when the window gets focus: checks for other devices'
// changes, at most once a minute.
func (a *App) SyncPoll() {
	if !a.store.Get().Sync.Enabled {
		return
	}
	a.syncTimerMu.Lock()
	recent := time.Since(a.syncPolled) < time.Minute
	if !recent {
		a.syncPolled = time.Now()
	}
	a.syncTimerMu.Unlock()
	if !recent {
		go a.syncCycle()
	}
}

// ---------- the Settings → Sync page ----------

// SyncStatus is what Settings → Sync shows.
type SyncStatus struct {
	Enabled       bool            `json:"enabled"`
	SignedIn      bool            `json:"signedIn"`
	Email         string          `json:"email"`
	Server        string          `json:"server"` // "" = irchatterbox.com
	DefaultServer string          `json:"defaultServer"`
	Categories    map[string]bool `json:"categories"`
	List          []SyncCategory  `json:"list"`
	LastSync      int64           `json:"lastSync"`
	LastError     string          `json:"lastError"`
	Devices       []SyncDevice    `json:"devices"`
}

func (a *App) GetSyncStatus() SyncStatus {
	sc := a.store.Get().Sync
	st := a.loadSyncState()
	cats := map[string]bool{}
	for _, c := range syncCategories {
		cats[c.ID] = sc.catOn(c.ID)
	}
	return SyncStatus{Enabled: sc.Enabled, SignedIn: a.syncToken() != "", Email: sc.Email, Server: sc.Server, DefaultServer: DefaultSyncServer,
		Categories: cats, List: syncCategories, LastSync: st.LastSync, LastError: st.LastError, Devices: st.Devices}
}

// SyncResult is what the sign-in and setup steps return.
type SyncResult struct {
	Email   string `json:"email,omitempty"`
	HasData bool   `json:"hasData,omitempty"` // the account already has settings from another device
	Code    string `json:"code,omitempty"`    // the new sync code (first device)
	Error   string `json:"error,omitempty"`
}

// SyncSignInStart emails a sign-in code. server is "" for irchatterbox.com, or the address
// of another server running the account site.
func (a *App) SyncSignInStart(server, email string) SyncResult {
	srv, err := cleanSyncServer(server)
	if err != nil {
		return SyncResult{Error: err.Error()}
	}
	email = strings.TrimSpace(email)
	if !strings.Contains(email, "@") {
		return SyncResult{Error: "Enter your email address"}
	}
	api := SyncConfig{Server: srv}.serverURL()
	var r struct {
		Pending string `json:"pending"`
	}
	if err := syncCall(api, "POST", "code", nil, "", map[string]string{"email": email}, &r); err != nil {
		return SyncResult{Error: err.Error()}
	}
	a.syncTimerMu.Lock()
	a.syncPending = syncPendingSignIn{server: srv, pending: r.Pending}
	a.syncTimerMu.Unlock()
	return SyncResult{}
}

// SyncSignInFinish checks the emailed code. On success this device is signed in (sync
// isn't on yet: the next step is SyncStart or SyncJoin) and HasData says which.
func (a *App) SyncSignInFinish(code string) SyncResult {
	a.syncTimerMu.Lock()
	p := a.syncPending
	a.syncTimerMu.Unlock()
	if p.pending == "" {
		return SyncResult{Error: "Ask for a code first"}
	}
	api := SyncConfig{Server: p.server}.serverURL()
	var r struct {
		Token string `json:"token"`
		Email string `json:"email"`
	}
	in := map[string]string{"pending": p.pending, "code": strings.TrimSpace(code), "platform": runtime.GOOS}
	if err := syncCall(api, "POST", "verify", nil, "", in, &r); err != nil {
		return SyncResult{Error: err.Error()}
	}
	a.syncTimerMu.Lock()
	a.syncPending = syncPendingSignIn{}
	a.syncTimerMu.Unlock()
	a.store.Update(func(c *Config) {
		c.Sync.Server, c.Sync.Email, c.Sync.Enabled = p.server, r.Email, false
		if c.Sync.DeviceID == "" {
			c.Sync.DeviceID = newID() + newID()
		}
	})
	a.setSyncToken(r.Token)
	f, err := accountTransport{api: api, token: r.Token}.Fetch("")
	if err != nil {
		return SyncResult{Email: r.Email, Error: err.Error()}
	}
	return SyncResult{Email: r.Email, HasData: len(f.Devices) > 0}
}

type syncPendingSignIn struct{ server, pending string }

func cleanCats(cats map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, c := range syncCategories {
		v, ok := cats[c.ID]
		if !ok {
			v = c.Default
		}
		out[c.ID] = v
	}
	return out
}

// SyncStart turns sync on for the first device of an account: makes the sync code, which
// is shown so other devices can join, and uploads this device's settings.
func (a *App) SyncStart(cats map[string]bool) SyncResult {
	tr, err := a.syncTransport(a.store.Get().Sync)
	if err != nil {
		return SyncResult{Error: err.Error()}
	}
	f, err := tr.Fetch("")
	if err != nil {
		return SyncResult{Error: err.Error()}
	}
	if len(f.Devices) > 0 {
		return SyncResult{Error: "This account already has synced settings from another device. Join with that device's sync code instead."}
	}
	code := newSyncCode()
	a.enableSync(code, cats, false)
	if err := a.syncCycle(); err != nil {
		return SyncResult{Code: code, Error: err.Error()}
	}
	return SyncResult{Code: code}
}

// SyncJoin turns sync on for another device, with the code from the first. On this first
// sync the other devices' settings win; what only this device has is added for everyone.
func (a *App) SyncJoin(code string, cats map[string]bool) SyncResult {
	norm, ok := normSyncCode(code)
	if !ok {
		return SyncResult{Error: "A sync code is 32 letters and numbers, like ABCD-EFGH-…"}
	}
	code = formatSyncCode(norm)
	tr, err := a.syncTransport(a.store.Get().Sync)
	if err != nil {
		return SyncResult{Error: err.Error()}
	}
	f, err := tr.Fetch("")
	if err != nil {
		return SyncResult{Error: err.Error()}
	}
	matched := false
	for _, b := range f.Blobs {
		if _, err := openSync(b.Data, code); err == nil {
			matched = true
			break
		}
	}
	if !matched {
		return SyncResult{Error: "That sync code doesn't match the settings on this account. Check it on your other device (Settings → Sync → Show sync code)."}
	}
	a.enableSync(code, cats, true)
	if err := a.syncCycle(); err != nil {
		return SyncResult{Error: err.Error()}
	}
	return SyncResult{}
}

func (a *App) enableSync(code string, cats map[string]bool, joining bool) {
	cats = cleanCats(cats)
	a.syncMu.Lock()
	st := &syncState{Items: map[string]syncStateItem{}, Servers: map[string]string{}, JoinCats: map[string]bool{}, Known: map[string]SyncDevice{}}
	if joining {
		for cat, on := range cats {
			st.JoinCats[cat] = on
		}
	}
	a.saveSyncState(st)
	a.syncMu.Unlock()
	a.setSyncCode(code)
	a.store.Update(func(c *Config) {
		c.Sync.Enabled, c.Sync.Categories = true, cats
	})
}

// SyncSetCategories changes what this device syncs. A category turned on takes the other
// devices' settings first.
func (a *App) SyncSetCategories(cats map[string]bool) {
	cats = cleanCats(cats)
	old := a.store.Get().Sync
	a.syncMu.Lock()
	st := a.loadSyncState()
	for cat, on := range cats {
		if on && !old.catOn(cat) {
			st.JoinCats[cat] = true
		}
	}
	st.Cursor = "" // read everything again: a newly chosen category needs the other devices' copies
	a.saveSyncState(st)
	a.syncMu.Unlock()
	a.store.Update(func(c *Config) { c.Sync.Categories = cats })
}

// SyncNow syncs straight away and returns the result.
func (a *App) SyncNow() SyncStatus {
	a.syncCycle()
	return a.GetSyncStatus()
}

// SyncGetCode shows this device's sync code, to set up another device.
func (a *App) SyncGetCode() string {
	if !a.store.Get().Sync.Enabled {
		return ""
	}
	return a.syncCode()
}

// SyncStop turns sync off on this device and signs it out: its file is removed from the
// account and the code forgotten. Settings stay as they are; other devices keep syncing.
func (a *App) SyncStop() {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	sc := a.store.Get().Sync
	if tok := a.syncToken(); tok != "" {
		tr := accountTransport{api: sc.serverURL(), token: tok}
		if sc.Enabled && sc.DeviceID != "" {
			tr.Remove(sc.DeviceID)
		}
		syncCall(tr.api, "POST", "signout", nil, tok, map[string]string{}, nil)
	}
	a.setSyncToken("")
	a.setSyncCode("")
	a.store.Update(func(c *Config) {
		c.Sync = SyncConfig{DeviceID: c.Sync.DeviceID, Categories: c.Sync.Categories, Server: c.Sync.Server}
	})
	os.Remove(a.syncStatePath())
}
