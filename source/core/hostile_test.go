package core

import (
	"bufio"
	"fmt"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// hostileConn is a connection whose server says whatever a test feeds it.
func hostileConn(t *testing.T) *Conn { return hostileConnFor(newBackupApp(t)) }

func hostileConnFor(a *App) *Conn {
	c := newConn(a, ServerConfig{ID: "s1", Nick: "me"})
	c.out = make(chan string, 1<<16)
	c.resetSessionLocked()
	c.nick, c.status, c.registered = "me", "connected", true
	return c
}

var hostileCmds = strings.Fields(`001 004 005 221 301 305 306 311 312 315 317 318 319 321 322 323 324 329 330 331 332 333 341
	352 353 354 366 367 368 372 375 376 391 401 403 404 421 432 433 437 471 473 474 475 477 482 671 730 731 732 733 734 900 901 902
	903 904 905 906 907 908 ACCOUNT ACK AUTHENTICATE AWAY BATCH CAP CHGHOST ERROR FAIL INVITE JOIN KICK KILL MARKREAD MODE NICK
	NOTE NOTICE PART PING PONG PRIVMSG QUIT REDACT SETNAME TAGMSG TOPIC WALLOPS WARN WHOX CHATHISTORY`)

var hostileParams = []string{"", ":", "*", "me", "#c", "#", "+", "-", "+o", "-o+v", "+b", "+k", "+l", "++", "@#c", "=", "-1",
	"99999999999999999999", "0", "\x01", "\x01DCC SEND", "\x01DCC SEND a 1 2 3\x01", "\x01DCC SEND \"x\" 0 0 -5\x01",
	"\x01DCC RESUME x 0 0\x01", "\x01DCC ACCEPT\x01", "\x01DCC CHAT chat 2130706433 22\x01", "\x01VERSION\x01", "\x01PING\x01",
	"\x01ACTION\x01", "\x01", "LS", "* LS :sasl", "ACK", "NAK", "LS * :", "NEW", "DEL", "+", "PREFIX=(ov)@", "PREFIX=(o)",
	"PREFIX=()", "PREFIX=(qaohv)", "CHANTYPES=", "CHANMODES=", "CHANMODES=,,,,,,,", "CHATHISTORY=x", "MULTILINE=max-bytes=0",
	"NETWORK=", "TARGMAX=PRIVMSG:", "@", "~&@%+", "@@@", "!", "!@", "nick!", "!user@host", "\xff\xfe", strings.Repeat("A", 600),
	"chathistory", "draft/multiline", "time=x", "+draft/react=", "../../x", `..\..\x`, "CON:", "%n%s"}

// A hostile or broken server must never crash the app: every command, with every number of
// odd parameters, from odd prefixes, with odd tags.
func TestHostileServerLines(t *testing.T) {
	prefixes := []string{"", ":", ":srv", ":me!u@h", ":bob!b@h", ":!@", ":bob", ":@", ":bob!"}
	tags := []string{"", "@", "@;", "@time=garbage", "@batch=x", "@batch=", "@msgid=;+draft/reply=", "@=;=;="}
	n := 0
	app := newBackupApp(t)
	for _, cmd := range hostileCmds {
		for np := 0; np <= 5; np++ {
			for pi, pre := range prefixes {
				for _, tg := range []string{"", tags[(np+pi)%len(tags)]} {
					for k := 0; k < len(hostileParams); k += 11 {
						c := hostileConnFor(app)
						c.channels["#c"] = &Channel{Name: "#c", Users: map[string]*User{"me": {Nick: "me"}, "bob": {Nick: "bob"}}}
						ps := make([]string, np)
						for i := range ps {
							ps[i] = hostileParams[(k+i*13+np)%len(hostileParams)]
						}
						line := strings.TrimSpace(strings.Join([]string{tg, pre, cmd}, " "))
						for i, p := range ps {
							if i == len(ps)-1 {
								line += " :" + p
							} else if p != "" && !strings.ContainsAny(p, " ") {
								line += " " + p
							}
						}
						func() {
							defer func() {
								if r := recover(); r != nil {
									t.Fatalf("panic on %q: %v\n%s", line, r, debug.Stack())
								}
							}()
							c.handle(ParseMessage(line))
						}()
						n++
					}
				}
			}
		}
	}
	t.Logf("%d lines", n)
}

// Sequences: things arriving in an order a client wouldn't expect.
func TestHostileServerSequences(t *testing.T) {
	seqs := [][]string{
		{":srv CAP * LS * :sasl", ":srv CAP * LS :", ":srv CAP * ACK :sasl", "AUTHENTICATE +", "AUTHENTICATE +", ":srv 903 me :ok", ":srv 904 me :no"},
		{":srv BATCH +a chathistory #c", ":srv BATCH +a chathistory #c", "@batch=a :bob!b@h PRIVMSG #c :hi", ":srv BATCH -a", ":srv BATCH -a", ":srv BATCH -zz"},
		{":srv BATCH +m draft/multiline #c", "@batch=m :bob!b@h PRIVMSG #c :" + strings.Repeat("x", 500), ":srv BATCH -m"},
		{":srv BATCH +m draft/multiline", "@batch=m;draft/multiline-concat :bob!b@h PRIVMSG #c :x", ":srv BATCH -m"},
		{":me!u@h JOIN #c", ":me!u@h PART #c", ":srv 353 me = #c :@me +bob", ":srv 366 me #c :end", ":srv 324 me #c +kl", ":srv 324 me #nope +l"},
		{":srv 005 me PREFIX=(ab)+ :x", ":srv 353 me = #c :+bob @me", ":srv MODE #c +ab me bob", ":srv MODE #c +ooooooooooo"},
		{":bob!b@h NICK :", ":bob!b@h NICK me", ":me!u@h NICK :", ":me!u@h QUIT", ":srv KICK #c", ":srv KICK #c me"},
		{":bob!b@h PRIVMSG me :\x01DCC SEND file 4294967295 65536 -1\x01", ":bob!b@h PRIVMSG me :\x01DCC SEND ../../../evil.exe 2130706433 0 10 5\x01",
			":bob!b@h PRIVMSG me :\x01DCC RESUME ../evil 0 99999999999\x01", ":bob!b@h PRIVMSG me :\x01DCC ACCEPT a b c\x01"},
	}
	for _, s := range seqs {
		c := hostileConn(t)
		c.channels["#c"] = &Channel{Name: "#c", Users: map[string]*User{}}
		for _, l := range s {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic on %q (in %v): %v\n%s", l, s, r, debug.Stack())
					}
				}()
				c.handle(ParseMessage(l))
			}()
		}
	}
}

func FuzzHandleLine(f *testing.F) {
	for _, s := range []string{":bob!b@h PRIVMSG #c :hi", "@time=x :srv 005 me PREFIX=(ov)@+", ":srv BATCH +a chathistory #c", ":bob PRIVMSG me :\x01DCC SEND a 1 2 3\x01"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		c := hostileConn(t)
		c.channels["#c"] = &Channel{Name: "#c", Users: map[string]*User{}}
		c.handle(ParseMessage(line))
	})
}

var _ = fmt.Sprint

func TestReadLineDropsHugeLines(t *testing.T) {
	huge := strings.Repeat("A", 5*maxLineBytes)
	r := bufio.NewReaderSize(strings.NewReader("PING :a\r\n"+huge+"\r\nPING :b\r\n"+huge), 4096)
	for _, want := range []string{"PING :a\r\n", "PING :b\r\n"} {
		got, err := readLine(r)
		if err != nil || got != want {
			t.Fatalf("got %q %v, want %q", got, err, want)
		}
	}
	if _, err := readLine(r); err == nil {
		t.Fatal("a line with no end should end in EOF, not be returned")
	}
	c := hostileConn(t)
	c.safeHandle("004") // used to panic; must not escape either way
}

func TestReservedWindowsNames(t *testing.T) {
	for in, want := range map[string]string{"nul": "_nul", "CON": "_CON", "com1": "_com1", "LPT9.log": "_LPT9.log", "con.tar.gz": "_con.tar.gz",
		"COM¹": "_COM¹", "conin$": "_conin$", "console": "console", "#nul": "#nul", "com10": "com10", "bob\x00x": "bob_x", "a/b": "a_b"} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"NUL.txt": "_NUL.txt", "aux.tar.gz": "_aux.tar.gz", "..\\..\\x.exe": "x.exe", "good.png": "good.png"} {
		if got := cleanFileName(in); got != want {
			t.Errorf("cleanFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCTCPAndDCCFloods(t *testing.T) {
	c := hostileConn(t)
	for i := 0; i < 40; i++ {
		c.handle(ParseMessage(fmt.Sprintf(":bot%d!b@h PRIVMSG me :\x01VERSION\x01", i)))
	}
	replies := 0
	for len(c.out) > 0 {
		if strings.Contains(<-c.out, "VERSION IRChatterbox") {
			replies++
		}
	}
	if replies == 0 || replies > ctcpBurst {
		t.Fatalf("%d CTCP replies to a flood", replies)
	}
	c.ctcpRecent = nil
	for i := 0; i < 20; i++ {
		c.ctcpRecent = nil // past the CTCP limit: test the offer limits on their own
		c.handle(ParseMessage(fmt.Sprintf(":bob!b@h PRIVMSG me :\x01DCC SEND f%d.txt 3232235777 5000 10\x01", i)))
	}
	if n, fromBob := dccPendingOffers("s1", "bob"); fromBob != dccMaxOffersPerNick || n != dccMaxOffersPerNick {
		t.Fatalf("pending %d / from bob %d", n, fromBob)
	}
	for _, bad := range []string{"2130706433 6379", "0 5000", "3232235777 445", "3232235777 22", "4294967295 5000"} {
		c.ctcpRecent = nil
		c.handle(ParseMessage(":eve!e@h PRIVMSG me :\x01DCC SEND evil.txt " + bad + " 10\x01"))
	}
	if _, fromEve := dccPendingOffers("s1", "eve"); fromEve != 0 {
		t.Fatalf("%d offers to local/service addresses accepted", fromEve)
	}
	c.ctcpRecent = nil
	c.handle(ParseMessage(":eve!e@h PRIVMSG me :\x01DCC SEND ok.txt 134744072 5000 10\x01"))
	if _, fromEve := dccPendingOffers("s1", "eve"); fromEve != 1 {
		t.Fatal("a normal public offer was refused")
	}
}

func TestUnclosedBatchesAreCapped(t *testing.T) {
	c := hostileConn(t)
	for i := 0; i < 500; i++ {
		c.handle(ParseMessage(fmt.Sprintf(":srv BATCH +m%d draft/multiline #c", i)))
		c.handle(ParseMessage(fmt.Sprintf(":srv BATCH +h%d chathistory #c", i)))
	}
	if len(c.mlBatches) > maxOpenMultiline || len(c.batches) > maxOpenBatches {
		t.Fatalf("%d multiline, %d batches open", len(c.mlBatches), len(c.batches))
	}
}

func TestIgnoreMasks(t *testing.T) {
	yes := [][2]string{{"troll!*@*", "troll!~t@user/troll"}, {"*!*@user/troll", "troll2!x@user/troll"}, {"*!*@*.example.net", "a!b@h1.example.net"},
		{"tr?ll!*@*", "troll!a@b"}, {"*", "anything!x@y"}, {"*troll*", "xtrollx!a@b"}}
	no := [][2]string{{"troll!*@*", "trolly!~t@user/troll"}, {"*!*@user/troll", "a!b@user/troll2"}, {"[", "x!y@z"}, {"", "x!y@z"}}
	for _, c := range yes {
		if !maskMatch(c[0], c[1]) {
			t.Errorf("%q should match %q", c[0], c[1])
		}
	}
	for _, c := range no {
		if maskMatch(c[0], c[1]) {
			t.Errorf("%q should not match %q", c[0], c[1])
		}
	}
}

// Two devices on one bouncer: a message typed on the phone reaches the computer as our own
// nick. Only the device that sent it may run bots on it, or both reply.
func TestBotsAnswerOnlyOnTheSendingDevice(t *testing.T) {
	for _, echo := range []bool{true, false} {
		c := hostileConn(t)
		c.app.store.Update(func(cf *Config) {
			cf.Plugins.WeatherEnabled, cf.Plugins.WeatherFromMe, cf.Plugins.WeatherCooldown, cf.Plugins.BotPrefix = true, true, 0, "[Bot] "
		})
		if echo {
			c.caps["echo-message"] = true
		}
		bots := func() int {
			time.Sleep(400 * time.Millisecond)
			n := 0
			for len(c.out) > 0 {
				if strings.Contains(<-c.out, "[Bot]") {
					n++
				}
			}
			return n
		}
		// sent from the other device: arrives from the server with our nick
		c.handle(ParseMessage(":me!u@h PRIVMSG #c :Feels like 1c brrr"))
		if n := bots(); n != 0 {
			t.Fatalf("echo=%v: replied %d time(s) to another device's message", echo, n)
		}
		// typed here
		c.Say("#c", "Feels like 2c brrr", false)
		if echo {
			for len(c.out) > 0 {
				<-c.out // the line went to the server…
			}
			c.handle(ParseMessage(":me!u@h PRIVMSG #c :Feels like 2c brrr")) // …and comes back
		}
		if n := bots(); n != 1 {
			t.Fatalf("echo=%v: replied %d time(s) to this device's own message, want 1", echo, n)
		}
	}
}

// Two devices on one bouncer both see someone else's message; only one of them may answer.
func TestBotsAnswerOthersOnceAcrossDevices(t *testing.T) {
	old := botWait
	defer func() { botWait = old }()
	for _, prefix := range []string{"[Bot] ", ""} {
		c := hostileConn(t)
		c.cfg.Bouncer = "soju"
		c.caps["echo-message"] = true
		c.app.store.Update(func(cf *Config) {
			cf.Plugins.WeatherEnabled, cf.Plugins.WeatherFromOthers, cf.Plugins.WeatherCooldown, cf.Plugins.BotPrefix = true, true, 0, prefix
		})
		sent := func(wait time.Duration) int {
			time.Sleep(wait)
			n := 0
			for len(c.out) > 0 {
				if strings.Contains(<-c.out, "4.4 C") {
					n++
				}
			}
			return n
		}
		// the other device answers while this one waits: this one stays quiet
		botWait = func() time.Duration { return 300 * time.Millisecond }
		c.handle(ParseMessage(":laura!u@h PRIVMSG #c :down to like 40f tonight"))
		time.Sleep(100 * time.Millisecond)
		c.handle(ParseMessage(":me!u@h PRIVMSG #c :" + prefix + "40 F = 4.4 C"))
		if n := sent(500 * time.Millisecond); n != 0 {
			t.Fatalf("prefix %q: answered %d time(s) after another device already had", prefix, n)
		}
		// nobody else answers: this one does, once
		c.handle(ParseMessage(":laura!u@h PRIVMSG #c :still 40f"))
		if n := sent(600 * time.Millisecond); n != 1 {
			t.Fatalf("prefix %q: answered %d time(s), want 1", prefix, n)
		}
		// an answer from before the trigger doesn't count
		c.handle(ParseMessage(":me!u@h PRIVMSG #c :" + prefix + "40 F = 4.4 C"))
		time.Sleep(50 * time.Millisecond)
		c.handle(ParseMessage(":laura!u@h PRIVMSG #c :40f again"))
		if n := sent(600 * time.Millisecond); n != 1 {
			t.Fatalf("prefix %q: an old answer stopped a new one (%d sent)", prefix, n)
		}
	}
}
