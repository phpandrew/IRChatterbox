package core

// DCC: direct file transfers (DCC SEND) and direct chats (DCC CHAT) between two people,
// outside the IRC server. Nothing is ever accepted without you clicking Accept.
//
// Offers arrive as CTCP "DCC SEND <file> <ip> <port> <size> [token]" or
// "DCC CHAT chat <ip> <port> [token]". Normally the sender listens and the receiver
// connects; with port 0 and a token ("passive"/reverse DCC, for senders behind a router)
// the receiver listens and answers with its own address and the same token.
//
// Your offers listen on a port from Settings → Media → DCC (or any free port). People
// behind a home router need that port forwarded for others to reach them.

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// DCCTransfer is what the UI shows for one offer, transfer or chat.
type DCCTransfer struct {
	ID      string `json:"id"`
	Server  string `json:"server"`
	Nick    string `json:"nick"`
	Kind    string `json:"kind"` // send | chat
	Dir     string `json:"dir"`  // in | out
	File    string `json:"file,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Done    int64  `json:"done,omitempty"`
	State   string `json:"state"` // offer | waiting | active | done | failed | cancelled | declined
	Error   string `json:"error,omitempty"`
	Path    string `json:"path,omitempty"` // where the file was saved / is read from
	Addr    string `json:"addr,omitempty"` // the other side's address
	Risky   bool   `json:"risky,omitempty"`
	Started int64  `json:"started,omitempty"`
	Time    int64  `json:"time"`
}

type dccItem struct {
	DCCTransfer
	c      *Conn
	ip     net.IP
	port   int
	token  string
	cancel context.CancelFunc
	chat   net.Conn // open DCC chat
	chatMu sync.Mutex
}

var (
	dccMu    sync.Mutex
	dccItems = map[string]*dccItem{}
)

const dccOfferTimeout = 3 * time.Minute

func newDCCID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *App) dccUpdate(it *dccItem) {
	dccMu.Lock()
	t := it.DCCTransfer
	dccMu.Unlock()
	a.emitUI("dcc", t)
}

// GetDCCTransfers lists offers and transfers from this session.
func (a *App) GetDCCTransfers() []DCCTransfer {
	dccMu.Lock()
	defer dccMu.Unlock()
	out := []DCCTransfer{}
	for _, it := range dccItems {
		out = append(out, it.DCCTransfer)
	}
	return out
}

// ---------- offers from others ----------

// parseDCCArgs splits the DCC arguments, allowing a "quoted file name with spaces".
func parseDCCArgs(s string) []string {
	var out []string
	s = strings.TrimSpace(s)
	for s != "" {
		if s[0] == '"' {
			if end := strings.IndexByte(s[1:], '"'); end >= 0 {
				out = append(out, s[1:1+end])
				s = strings.TrimSpace(s[2+end:])
				continue
			}
		}
		f, rest, _ := strings.Cut(s, " ")
		out = append(out, f)
		s = strings.TrimSpace(rest)
	}
	return out
}

// parseDCCIP reads the address in an offer: a 32-bit number (IPv4) or a literal IP.
func parseDCCIP(s string) net.IP {
	if n, err := strconv.ParseUint(s, 10, 32); err == nil {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, uint32(n))
		return ip
	}
	return net.ParseIP(s)
}

// cleanFileName makes an offered name safe to save: no folders, no characters Windows
// refuses, no reserved device names.
func cleanFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = name[strings.LastIndexByte(name, '/')+1:]
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "file"
	}
	base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	if len(name) > 180 {
		ext := filepath.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		name = name[:180-len(ext)] + ext
	}
	return name
}

var riskyExt = map[string]bool{
	".exe": true, ".scr": true, ".bat": true, ".cmd": true, ".com": true, ".pif": true, ".msi": true, ".msix": true,
	".js": true, ".jse": true, ".vbs": true, ".vbe": true, ".wsf": true, ".ps1": true, ".hta": true, ".lnk": true,
	".jar": true, ".dll": true, ".reg": true, ".apk": true, ".app": true, ".dmg": true, ".pkg": true, ".sh": true,
	".docm": true, ".xlsm": true, ".iso": true,
}

// onDCC handles a CTCP DCC request from nick.
func (c *Conn) onDCC(nick, prefix, arg string) {
	args := parseDCCArgs(arg)
	if len(args) < 4 {
		return
	}
	kind := strings.ToUpper(args[0])
	it := &dccItem{c: c}
	it.ID, it.Server, it.Nick, it.Dir, it.State, it.Time = newDCCID(), c.cfg.ID, nick, "in", "offer", time.Now().UnixMilli()
	switch kind {
	case "SEND":
		if len(args) < 5 {
			return
		}
		it.Kind = "send"
		it.File = cleanFileName(args[1])
		it.ip = parseDCCIP(args[2])
		it.port, _ = strconv.Atoi(args[3])
		it.Size, _ = strconv.ParseInt(args[4], 10, 64)
		if len(args) > 5 {
			it.token = args[5]
		}
		it.Risky = riskyExt[strings.ToLower(filepath.Ext(it.File))]
	case "CHAT":
		it.Kind = "chat"
		it.ip = parseDCCIP(args[2])
		it.port, _ = strconv.Atoi(args[3])
		if len(args) > 4 {
			it.token = args[4]
		}
	case "ACCEPT", "RESUME":
		return // resuming isn't supported
	default:
		return
	}
	if it.ip == nil || it.port < 0 || it.port > 65535 || it.Size < 0 || (it.port == 0 && it.token == "") {
		return
	}
	// the answer to one of our own passive offers would come here; we don't make those
	if it.port == 0 {
		it.Addr = "(they're behind a router: you'll listen for them)"
	} else {
		it.Addr = net.JoinHostPort(it.ip.String(), strconv.Itoa(it.port))
	}
	dccMu.Lock()
	dccItems[it.ID] = it
	dccMu.Unlock()
	what := "a direct chat"
	if it.Kind == "send" {
		what = fmt.Sprintf("the file %s (%s)", it.File, humanBytes(it.Size))
	}
	c.info(nick, "info", fmt.Sprintf("%s wants to send you %s by DCC. Accept or decline it below, or with /dcc get %s.", nick, what, nick))
	c.app.dccUpdate(it)
	c.app.notify(c, nick, nick, "DCC: wants to send you "+what, true, false)
	// offers that are never answered go away
	time.AfterFunc(dccOfferTimeout+time.Minute, func() {
		dccMu.Lock()
		expired := it.State == "offer"
		if expired {
			it.State, it.Error = "failed", "the offer ran out"
		}
		dccMu.Unlock()
		if expired {
			c.app.dccUpdate(it)
		}
	})
}

// DCCAccept accepts an offer. Returns an error message or "".
func (a *App) DCCAccept(id string) string {
	dccMu.Lock()
	it := dccItems[id]
	if it == nil || it.State != "offer" || it.Dir != "in" {
		dccMu.Unlock()
		return "That offer is no longer available"
	}
	ctx, cancel := context.WithCancel(context.Background())
	it.cancel = cancel
	it.State = "waiting"
	dccMu.Unlock()
	a.dccUpdate(it)
	go a.dccReceive(ctx, it)
	return ""
}

// DCCDecline turns an offer down, or cancels a transfer or chat in progress.
func (a *App) DCCDecline(id string) {
	dccMu.Lock()
	it := dccItems[id]
	if it == nil {
		dccMu.Unlock()
		return
	}
	switch it.State {
	case "offer":
		it.State = "declined"
	case "waiting", "active":
		it.State = "cancelled"
	default:
		dccMu.Unlock()
		return
	}
	if it.cancel != nil {
		it.cancel()
	}
	if it.chat != nil {
		it.chat.Close()
	}
	dccMu.Unlock()
	a.dccUpdate(it)
}

// dccReceive connects to the sender (or, for passive offers, listens for them), then
// receives the file or runs the chat.
func (a *App) dccReceive(ctx context.Context, it *dccItem) {
	var conn net.Conn
	var err error
	if it.port == 0 {
		conn, err = a.dccListenFor(ctx, it, func(ip string, port int) {
			if it.Kind == "send" {
				it.c.SendRaw(fmt.Sprintf("PRIVMSG %s :\x01DCC SEND %s %s %d %d %s\x01", it.Nick, dccQuote(it.File), ip, port, it.Size, it.token))
			} else {
				it.c.SendRaw(fmt.Sprintf("PRIVMSG %s :\x01DCC CHAT chat %s %d %s\x01", it.Nick, ip, port, it.token))
			}
		})
	} else {
		d := net.Dialer{Timeout: 20 * time.Second}
		conn, err = d.DialContext(ctx, "tcp", net.JoinHostPort(it.ip.String(), strconv.Itoa(it.port)))
	}
	if err != nil {
		a.dccFail(it, err)
		return
	}
	if it.Kind == "chat" {
		a.dccRunChat(ctx, it, conn)
		return
	}
	defer conn.Close()
	a.dccDownload(ctx, it, conn)
}

func (a *App) dccFail(it *dccItem, err error) {
	dccMu.Lock()
	if it.State == "cancelled" || it.State == "declined" {
		dccMu.Unlock()
		return
	}
	it.State = "failed"
	it.Error = dccErrText(err)
	dccMu.Unlock()
	a.dccUpdate(it)
	it.c.info(it.Nick, "error", "DCC with "+it.Nick+" failed: "+it.Error)
}

func dccErrText(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.As(err, &ne) && ne.Timeout():
		return "couldn't connect in time (a router or firewall may be in the way)"
	case strings.Contains(err.Error(), "refused"):
		return "the other side refused the connection"
	}
	return err.Error()
}

// dccFolder is where received files go: Settings → DCC folder, or Downloads.
func (a *App) dccFolder() string {
	if d := strings.TrimSpace(a.store.Get().Settings.DCCFolder); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		if st, err := os.Stat(filepath.Join(home, "Downloads")); err == nil && st.IsDir() {
			return filepath.Join(home, "Downloads")
		}
	}
	return filepath.Join(a.store.dir, "downloads")
}

// freeName returns dir/name, or "name (1).ext" etc. if that's taken.
func freeName(dir, name string) string {
	p := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
}

func (a *App) dccDownload(ctx context.Context, it *dccItem, conn net.Conn) {
	dir := a.dccFolder()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.dccFail(it, err)
		return
	}
	path := freeName(dir, it.File)
	part := path + ".part"
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		a.dccFail(it, err)
		return
	}
	dccMu.Lock()
	it.State, it.Path, it.Started = "active", path, time.Now().UnixMilli()
	dccMu.Unlock()
	a.dccUpdate(it)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	buf := make([]byte, 64*1024)
	var got int64
	var ack [4]byte
	lastUI := time.Now()
	for it.Size == 0 || got < it.Size {
		conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		n, rerr := conn.Read(buf)
		if n > 0 {
			if it.Size > 0 && got+int64(n) > it.Size {
				n = int(it.Size - got) // never take more than was offered
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(part)
				a.dccFail(it, werr)
				return
			}
			got += int64(n)
			binary.BigEndian.PutUint32(ack[:], uint32(got)) // classic DCC acknowledgement
			conn.Write(ack[:])
			if time.Since(lastUI) > 300*time.Millisecond {
				lastUI = time.Now()
				dccMu.Lock()
				it.Done = got
				dccMu.Unlock()
				a.dccUpdate(it)
			}
		}
		if rerr != nil {
			if rerr == io.EOF && (it.Size == 0 || got == it.Size) {
				break
			}
			f.Close()
			os.Remove(part)
			if rerr == io.EOF {
				rerr = fmt.Errorf("the sender stopped after %s of %s", humanBytes(got), humanBytes(it.Size))
			}
			if ctx.Err() != nil {
				rerr = ctx.Err()
			}
			a.dccFail(it, rerr)
			return
		}
	}
	f.Close()
	if err := os.Rename(part, path); err != nil {
		a.dccFail(it, err)
		return
	}
	dccMu.Lock()
	it.State, it.Done = "done", got
	dccMu.Unlock()
	a.dccUpdate(it)
	it.c.info(it.Nick, "info", fmt.Sprintf("Received %s from %s (%s), saved to %s", it.File, it.Nick, humanBytes(got), path))
}

// ---------- offers to others ----------

// dccAddress is the address to put in our offers: the setting, or this computer's
// address on the internet.
func (a *App) dccAddress(c *Conn) (net.IP, error) {
	if s := strings.TrimSpace(a.store.Get().Settings.DCCAddress); s != "" {
		if ip := net.ParseIP(s); ip != nil {
			return ip, nil
		}
		ips, err := net.LookupIP(s)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("couldn't look up the DCC address %q", s)
		}
		return ips[0], nil
	}
	var local net.IP
	c.mu.Lock()
	if c.conn != nil {
		if ta, ok := c.conn.LocalAddr().(*net.TCPAddr); ok {
			local = ta.IP
		}
	}
	c.mu.Unlock()
	if local != nil && isPublicIP(local) {
		return local, nil
	}
	if ip := publicIPv4(); ip != nil {
		return ip, nil
	}
	if local != nil {
		return local, nil
	}
	return nil, errors.New("couldn't work out your address; set it in Settings → Media → DCC")
}

var (
	pubIPMu sync.Mutex
	pubIP   net.IP
	pubIPAt time.Time
)

// publicIPv4 asks a "what's my IP" service, remembering the answer for an hour.
func publicIPv4() net.IP {
	pubIPMu.Lock()
	defer pubIPMu.Unlock()
	if pubIP != nil && time.Since(pubIPAt) < time.Hour {
		return pubIP
	}
	cl := &http.Client{Timeout: 6 * time.Second}
	resp, err := cl.Get("https://api.ipify.org")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	ip := net.ParseIP(strings.TrimSpace(string(b)))
	if ip == nil || ip.To4() == nil {
		return nil
	}
	pubIP, pubIPAt = ip.To4(), time.Now()
	return pubIP
}

// dccListen opens a listening port from the configured range ("5000-5010"), or any.
func (a *App) dccListen() (net.Listener, error) {
	spec := strings.TrimSpace(a.store.Get().Settings.DCCPorts)
	if spec == "" {
		return net.Listen("tcp", ":0")
	}
	lo, hi, found := strings.Cut(spec, "-")
	from, err1 := strconv.Atoi(strings.TrimSpace(lo))
	to := from
	var err2 error
	if found {
		to, err2 = strconv.Atoi(strings.TrimSpace(hi))
	}
	if err1 != nil || err2 != nil || from < 1 || to > 65535 || to < from {
		return nil, fmt.Errorf("the DCC ports setting %q isn't a port or range like 5000-5010", spec)
	}
	var last error
	for p := from; p <= to; p++ {
		l, err := net.Listen("tcp", ":"+strconv.Itoa(p))
		if err == nil {
			return l, nil
		}
		last = err
	}
	return nil, fmt.Errorf("no free DCC port in %s: %v", spec, last)
}

// dccListenFor listens, tells the other side where (announce), and waits for them.
func (a *App) dccListenFor(ctx context.Context, it *dccItem, announce func(ip string, port int)) (net.Conn, error) {
	ip, err := a.dccAddress(it.c)
	if err != nil {
		return nil, err
	}
	l, err := a.dccListen()
	if err != nil {
		return nil, err
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	ipText := ip.String()
	if v4 := ip.To4(); v4 != nil {
		ipText = strconv.FormatUint(uint64(binary.BigEndian.Uint32(v4)), 10)
	}
	announce(ipText, port)
	ctx, cancel := context.WithTimeout(ctx, dccOfferTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { l.Close() })
	defer stop()
	conn, err := l.Accept()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, errors.New("they didn't connect in time (they may have declined, or port " + strconv.Itoa(port) + " isn't reachable from the internet)")
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return conn, nil
}

func dccQuote(name string) string {
	if strings.ContainsAny(name, " \"") {
		return `"` + strings.ReplaceAll(name, `"`, "'") + `"`
	}
	return name
}

// DCCSend offers a file to nick. Returns an error message or "".
func (a *App) DCCSend(server, nick, path string) string {
	c := a.conn(server)
	if c == nil || c.Status() != "connected" {
		return "Not connected"
	}
	if nick == "" || c.isChannel(nick) || strings.ContainsAny(nick, " ,*") {
		return "Files can only be sent to one person"
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "Couldn't open that file"
	}
	it := &dccItem{c: c}
	it.ID, it.Server, it.Nick, it.Kind, it.Dir, it.State, it.Time = newDCCID(), server, nick, "send", "out", "waiting", time.Now().UnixMilli()
	it.File, it.Size, it.Path = cleanFileName(filepath.Base(path)), st.Size(), path
	ctx, cancel := context.WithCancel(context.Background())
	it.cancel = cancel
	dccMu.Lock()
	dccItems[it.ID] = it
	dccMu.Unlock()
	a.dccUpdate(it)
	c.info(nick, "info", fmt.Sprintf("Offering %s (%s) to %s. Waiting for them to accept…", it.File, humanBytes(it.Size), nick))
	go func() {
		conn, err := a.dccListenFor(ctx, it, func(ip string, port int) {
			c.SendRaw(fmt.Sprintf("PRIVMSG %s :\x01DCC SEND %s %s %d %d\x01", nick, dccQuote(it.File), ip, port, it.Size))
		})
		if err != nil {
			a.dccFail(it, err)
			return
		}
		defer conn.Close()
		a.dccUpload(ctx, it, conn)
	}()
	return ""
}

func (a *App) dccUpload(ctx context.Context, it *dccItem, conn net.Conn) {
	f, err := os.Open(it.Path)
	if err != nil {
		a.dccFail(it, err)
		return
	}
	defer f.Close()
	dccMu.Lock()
	it.State, it.Started, it.Addr = "active", time.Now().UnixMilli(), conn.RemoteAddr().String()
	dccMu.Unlock()
	a.dccUpdate(it)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	// the receiver acknowledges with the running total; it's done when that reaches the size
	acked := make(chan struct{})
	go func() {
		var b [4]byte
		for {
			conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
			if _, err := io.ReadFull(conn, b[:]); err != nil {
				close(acked)
				return
			}
			if int64(binary.BigEndian.Uint32(b[:])) == it.Size&0xffffffff {
				close(acked)
				return
			}
		}
	}()
	buf := make([]byte, 64*1024)
	var sent int64
	lastUI := time.Now()
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			conn.SetWriteDeadline(time.Now().Add(2 * time.Minute))
			if _, werr := conn.Write(buf[:n]); werr != nil {
				if ctx.Err() != nil {
					werr = ctx.Err()
				}
				a.dccFail(it, werr)
				return
			}
			sent += int64(n)
			if time.Since(lastUI) > 300*time.Millisecond {
				lastUI = time.Now()
				dccMu.Lock()
				it.Done = sent
				dccMu.Unlock()
				a.dccUpdate(it)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			a.dccFail(it, rerr)
			return
		}
	}
	select {
	case <-acked:
	case <-time.After(30 * time.Second):
	case <-ctx.Done():
	}
	dccMu.Lock()
	it.State, it.Done = "done", sent
	dccMu.Unlock()
	a.dccUpdate(it)
	it.c.info(it.Nick, "info", fmt.Sprintf("Sent %s to %s (%s)", it.File, it.Nick, humanBytes(sent)))
}

// ---------- chat ----------

// DCCChat offers a direct chat to nick. Returns an error message or "".
func (a *App) DCCChat(server, nick string) string {
	c := a.conn(server)
	if c == nil || c.Status() != "connected" {
		return "Not connected"
	}
	if nick == "" || c.isChannel(nick) || strings.ContainsAny(nick, " ,*") {
		return "A direct chat is with one person"
	}
	it := &dccItem{c: c}
	it.ID, it.Server, it.Nick, it.Kind, it.Dir, it.State, it.Time = newDCCID(), server, nick, "chat", "out", "waiting", time.Now().UnixMilli()
	ctx, cancel := context.WithCancel(context.Background())
	it.cancel = cancel
	dccMu.Lock()
	dccItems[it.ID] = it
	dccMu.Unlock()
	a.dccUpdate(it)
	c.info(nick, "info", "Offering "+nick+" a direct chat. Waiting for them to accept…")
	go func() {
		conn, err := a.dccListenFor(ctx, it, func(ip string, port int) {
			c.SendRaw(fmt.Sprintf("PRIVMSG %s :\x01DCC CHAT chat %s %d\x01", nick, ip, port))
		})
		if err != nil {
			a.dccFail(it, err)
			return
		}
		a.dccRunChat(ctx, it, conn)
	}()
	return ""
}

func dccChatBuffer(nick string) string { return "=" + nick }

// dccRunChat shows the chat's lines in the "=nick" window until either side closes it.
func (a *App) dccRunChat(ctx context.Context, it *dccItem, conn net.Conn) {
	buf := dccChatBuffer(it.Nick)
	dccMu.Lock()
	// one chat per person: a new one replaces the old
	for _, o := range dccItems {
		if o != it && o.Kind == "chat" && o.Server == it.Server && strings.EqualFold(o.Nick, it.Nick) && o.chat != nil {
			o.chat.Close()
		}
	}
	it.chat, it.State, it.Started, it.Addr = conn, "active", time.Now().UnixMilli(), conn.RemoteAddr().String()
	dccMu.Unlock()
	a.dccUpdate(it)
	c := it.c
	c.emit(Event{Type: "buffer", Buffer: buf, Extra: map[string]any{"action": "open", "focus": true}})
	c.info(buf, "info", "Direct chat with "+it.Nick+" connected ("+it.Addr+"). Messages here don't go through the IRC server.")
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 4096), 64*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		kind := "privmsg"
		if strings.HasPrefix(line, "\x01ACTION ") {
			kind, line = "action", strings.TrimSuffix(strings.TrimPrefix(line, "\x01ACTION "), "\x01")
		}
		hl := a.isHighlight(c.Nick(), line)
		c.emit(Event{Type: "msg", Buffer: buf, Kind: kind, Nick: it.Nick, Text: line, Highlight: hl, Extra: map[string]any{"dcc": true}})
		a.notify(c, buf, it.Nick, line, true, hl)
	}
	conn.Close()
	dccMu.Lock()
	it.chat = nil
	if it.State == "active" {
		it.State = "done"
	}
	dccMu.Unlock()
	a.dccUpdate(it)
	c.info(buf, "info", "Direct chat with "+it.Nick+" closed.")
}

// dccChatSay sends a line in an open direct chat. Returns false if there isn't one.
func (a *App) dccChatSay(c *Conn, buffer, kind, text string) bool {
	nick := strings.TrimPrefix(buffer, "=")
	dccMu.Lock()
	var it *dccItem
	for _, o := range dccItems {
		if o.Kind == "chat" && o.Server == c.cfg.ID && strings.EqualFold(o.Nick, nick) && o.chat != nil {
			it = o
		}
	}
	dccMu.Unlock()
	if it == nil {
		c.info(buffer, "error", "This direct chat isn't connected. Start a new one with /dcc chat "+nick)
		return false
	}
	line := text
	if kind == "action" {
		line = "\x01ACTION " + text + "\x01"
	}
	it.chatMu.Lock()
	it.chat.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, err := io.WriteString(it.chat, line+"\n")
	it.chatMu.Unlock()
	if err != nil {
		c.info(buffer, "error", "Couldn't send: "+err.Error())
		return false
	}
	c.emit(Event{Type: "msg", Buffer: buffer, Kind: kind, Nick: c.Nick(), Text: text, Self: true, Extra: map[string]any{"dcc": true}})
	return true
}

// ---------- /dcc ----------

// dccCommand runs /dcc send|chat|get|close <nick> [file].
func (a *App) dccCommand(c *Conn, buffer, args string) {
	sub, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	nick, file, _ := strings.Cut(strings.TrimSpace(rest), " ")
	file = strings.Trim(strings.TrimSpace(file), `"`)
	if nick == "" && !c.isChannel(buffer) && buffer != "" && !strings.HasPrefix(buffer, "*") {
		nick = strings.TrimPrefix(buffer, "=")
	}
	switch strings.ToLower(sub) {
	case "send":
		if nick == "" {
			c.info(buffer, "error", "Usage: /dcc send <nick> [file]")
			return
		}
		if file == "" {
			a.emitUI("dccPick", map[string]any{"server": c.cfg.ID, "nick": nick})
			return
		}
		if err := a.DCCSend(c.cfg.ID, nick, file); err != "" {
			c.info(buffer, "error", err)
		}
	case "chat":
		if nick == "" {
			c.info(buffer, "error", "Usage: /dcc chat <nick>")
			return
		}
		if err := a.DCCChat(c.cfg.ID, nick); err != "" {
			c.info(buffer, "error", err)
		}
	case "get", "accept":
		if id := a.dccFind(c, nick, "offer"); id != "" {
			if err := a.DCCAccept(id); err != "" {
				c.info(buffer, "error", err)
			}
		} else {
			c.info(buffer, "error", "No DCC offer from "+nick)
		}
	case "close", "decline", "cancel":
		if id := a.dccFind(c, nick, ""); id != "" {
			a.DCCDecline(id)
		} else {
			c.info(buffer, "error", "Nothing to close with "+nick)
		}
	default:
		c.info(buffer, "info", "/dcc send <nick> [file] · /dcc chat <nick> · /dcc get <nick> · /dcc close <nick>")
	}
}

// dccFind returns the newest offer/transfer with nick in the given state ("" = any open one).
func (a *App) dccFind(c *Conn, nick, state string) string {
	dccMu.Lock()
	defer dccMu.Unlock()
	best, at := "", int64(0)
	for id, it := range dccItems {
		if it.Server != c.cfg.ID || !strings.EqualFold(it.Nick, nick) || it.Time < at {
			continue
		}
		open := it.State == "offer" || it.State == "waiting" || it.State == "active"
		if (state != "" && it.State == state) || (state == "" && open) {
			best, at = id, it.Time
		}
	}
	return best
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
