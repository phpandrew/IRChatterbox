package core

// mIRC scripting (.mrc): a compatible core of mIRC's scripting language, so the scripts
// people already have keep working. Files go in the plugins folder next to the .js plugins
// and load with /reload.
//
// Supported: alias (also called as $alias(…) identifiers), on TEXT/ACTION/NOTICE/JOIN/PART/
// QUIT/KICK/NICK/TOPIC/INVITE/INPUT/CONNECT/DISCONNECT/START/LOAD/RAWMODE events, raw
// events, if/elseif/else, while, %variables (global, saved; and local with var), hash
// tables, timers, and the common identifiers. Not supported: dialogs, sockets, file and DLL
// access, /run, custom windows, menus — they're skipped when a script is loaded.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

type mircEvent struct {
	file    string
	kind    string // TEXT, JOIN, … or RAW
	match   string // match text (TEXT/ACTION/NOTICE/RAW), wildcard or /regex/
	regex   *regexp.Regexp
	target  string // #, ?, *, #chan,#chan2
	me      int    // 1 = only for me, -1 = never for me ("!" level), 0 = anyone
	needOp  bool   // "@" level: only when you're an op in the channel
	body    []mStmt
	rawCode string
}

type mircAlias struct {
	file string
	body []mStmt
}

type mircTimer struct {
	name  string
	stop  chan struct{}
	conn  *Conn
	reps  int
	delay time.Duration
	cmd   string
	ctx   *mCtx
}

type mircEngine struct {
	p          *Plugins
	mu         sync.Mutex // one script runs at a time, like mIRC
	files      []string
	aliases    map[string]*mircAlias
	events     []*mircEvent
	vars       map[string]string
	hash       map[string]map[string]string
	timers     map[string]*mircTimer
	timerN     int
	saveT      *time.Timer
	jobs       chan func()
	nEvents    atomic.Int32
	varsLoaded bool
	menus      []*mircMenu
	menuNodes  []*mircMenuNode
}

func newMircEngine(p *Plugins) *mircEngine {
	e := &mircEngine{p: p, aliases: map[string]*mircAlias{}, vars: map[string]string{}, hash: map[string]map[string]string{}, timers: map[string]*mircTimer{}, jobs: make(chan func(), 1000)}
	go e.worker()
	return e
}

// ---------- statements ----------

type mStmt struct {
	kind  string // cmd, if, while
	text  string // command line, or condition for if/while
	body  []mStmt
	elifs []mStmt // for "if": elseif branches (kind "elseif") and a final "else"
}

// ---------- loading ----------

func (e *mircEngine) load(dir string) (names, errs []string) {
	e.stopTimers()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.aliases = map[string]*mircAlias{}
	e.events = nil
	e.files = nil
	e.menus, e.menuNodes = nil, nil
	e.loadVars(dir)
	files, _ := filepath.Glob(filepath.Join(dir, "*.mrc"))
	sort.Strings(files)
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasPrefix(base, "_") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		warn := e.parseFile(base, string(src))
		names = append(names, base)
		e.files = append(e.files, base)
		for _, w := range warn {
			errs = append(errs, base+": "+w)
		}
	}
	e.nEvents.Store(int32(len(e.events)))
	return
}

// stripComments removes ; comment lines and /* */ blocks.
func stripComments(src string) []string {
	var out []string
	inBlock := false
	for _, l := range strings.Split(strings.ReplaceAll(src, "\r", ""), "\n") {
		t := strings.TrimSpace(l)
		if inBlock {
			if strings.HasPrefix(t, "*/") || strings.HasSuffix(t, "*/") {
				inBlock = false
			}
			continue
		}
		if strings.HasPrefix(t, "/*") {
			if !strings.HasSuffix(t, "*/") || t == "/*" {
				inBlock = true
			}
			continue
		}
		if strings.HasPrefix(t, ";") || t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func braceDelta(line string) int {
	d := 0
	for _, tok := range strings.Fields(line) {
		switch {
		case tok == "{":
			d++
		case tok == "}":
			d--
		case strings.HasSuffix(tok, ":{") && d == 0:
			d++
		}
	}
	return d
}

// parseFile splits a file into top-level definitions (alias, on, raw; others skipped).
func (e *mircEngine) parseFile(file, src string) (warn []string) {
	lines := stripComments(src)
	for i := 0; i < len(lines); i++ {
		head := lines[i]
		depth := braceDelta(head)
		block := []string{head}
		for depth > 0 && i+1 < len(lines) {
			i++
			block = append(block, lines[i])
			depth += braceDelta(lines[i])
		}
		def := strings.Join(block, "\n")
		word := strings.ToLower(strings.Fields(head)[0])
		switch word {
		case "alias":
			if err := e.parseAlias(file, def); err != nil {
				warn = append(warn, err.Error())
			}
		case "on":
			if err := e.parseOn(file, def); err != nil {
				warn = append(warn, err.Error())
			}
		case "raw":
			if err := e.parseRaw(file, def); err != nil {
				warn = append(warn, err.Error())
			}
		case "menu":
			if err := e.parseMenu(file, def); err != nil {
				warn = append(warn, err.Error())
			}
		case "dialog", "ctcp", "#":
			// not supported: skipped
		default:
			if !strings.HasPrefix(word, "#") {
				warn = append(warn, "skipped line: "+clip(head, 60))
			}
		}
	}
	return
}

// splitBody takes "header { … }" or "header cmd" and returns the statement list.
func bodyOf(rest string) []mStmt {
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "{") {
		rest = strings.TrimSpace(rest[1:])
		if i := strings.LastIndex(rest, "}"); i >= 0 {
			rest = rest[:i]
		}
	}
	return parseStmts(tokenizeBody(rest))
}

func (e *mircEngine) parseAlias(file, def string) error {
	rest := strings.TrimSpace(def[len("alias"):])
	f := strings.Fields(rest)
	if len(f) > 0 && strings.HasPrefix(f[0], "-") {
		rest = strings.TrimSpace(rest[len(f[0]):])
	}
	name, body, _ := strings.Cut(rest, " ")
	name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "/")))
	if name == "" {
		return errors.New("alias without a name")
	}
	if strings.HasSuffix(name, "{") { // alias name{
		name = strings.TrimSuffix(name, "{")
		body = "{" + body
	}
	e.aliases[name] = &mircAlias{file: file, body: bodyOf(body)}
	return nil
}

var mircEventFields = map[string]int{ // fields between EVENT and the commands
	"TEXT": 2, "ACTION": 2, "NOTICE": 2,
	"JOIN": 1, "PART": 1, "KICK": 1, "TOPIC": 1, "INVITE": 1, "INPUT": 1, "RAWMODE": 1,
	"QUIT": 0, "NICK": 0, "CONNECT": 0, "DISCONNECT": 0, "START": 0, "LOAD": 0,
}

func (e *mircEngine) parseOn(file, def string) error {
	rest := strings.TrimSpace(def[len("on"):])
	ev := &mircEvent{file: file}
	// level: "*", "1", "me:*", "!*", "$*" (regex match), "@*" (ignored)
	if strings.HasPrefix(strings.ToLower(rest), "me:") {
		ev.me = 1
		rest = rest[3:]
	}
	level, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return errors.New("bad event: " + clip(def, 60))
	}
	isRegex := strings.Contains(level, "$")
	ev.needOp = strings.Contains(level, "@")
	if strings.HasPrefix(level, "!") {
		ev.me = -1
	}
	kind, rest, _ := strings.Cut(rest, ":")
	ev.kind = strings.ToUpper(strings.TrimSpace(kind))
	n, known := mircEventFields[ev.kind]
	if !known {
		return fmt.Errorf("event %s isn't supported (skipped)", ev.kind)
	}
	parts := strings.SplitN(rest, ":", n+1)
	if len(parts) < n+1 {
		return errors.New("bad event: " + clip(def, 60))
	}
	if n == 2 {
		ev.match, ev.target = parts[0], parts[1]
	} else if n == 1 {
		ev.target = parts[0]
	}
	if isRegex && ev.match != "" {
		if re, err := compileMircRegex(ev.match); err == nil {
			ev.regex = re
		} else {
			return fmt.Errorf("bad regex %q: %v", ev.match, err)
		}
	}
	ev.body = bodyOf(parts[n])
	e.events = append(e.events, ev)
	return nil
}

func (e *mircEngine) parseRaw(file, def string) error {
	rest := strings.TrimSpace(def[len("raw"):])
	code, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return errors.New("bad raw event")
	}
	match, body, ok := strings.Cut(rest, ":")
	if !ok {
		return errors.New("bad raw event")
	}
	e.events = append(e.events, &mircEvent{file: file, kind: "RAW", rawCode: strings.ToUpper(strings.TrimSpace(code)), match: match, body: bodyOf(body)})
	return nil
}

func compileMircRegex(s string) (*regexp.Regexp, error) {
	flags := ""
	if strings.HasPrefix(s, "/") {
		if i := strings.LastIndex(s, "/"); i > 0 {
			flags, s = s[i+1:], s[1:i]
		}
	}
	if strings.Contains(flags, "i") {
		s = "(?i)" + s
	}
	return regexp.Compile(s)
}

// ---------- body tokenizer and parser ----------

type mTok struct {
	kind string // cmd, open, close
	text string
}

// tokenizeBody turns a body into commands and braces: newlines and " | " separate commands.
func tokenizeBody(body string) []mTok {
	var toks []mTok
	for _, line := range strings.Split(body, "\n") {
		for _, part := range splitPipes(line) {
			var cur []string
			flush := func() {
				if t := strings.TrimSpace(strings.Join(cur, " ")); t != "" {
					toks = append(toks, mTok{"cmd", t})
				}
				cur = nil
			}
			for _, w := range strings.Fields(part) {
				switch w {
				case "{":
					flush()
					toks = append(toks, mTok{"open", ""})
				case "}":
					flush()
					toks = append(toks, mTok{"close", ""})
				default:
					cur = append(cur, w)
				}
			}
			flush()
		}
	}
	return toks
}

// splitPipes splits on " | " (mIRC's command separator), not on | inside words.
func splitPipes(line string) []string {
	var out []string
	for {
		i := strings.Index(line, " | ")
		if i < 0 {
			return append(out, line)
		}
		out = append(out, line[:i])
		line = line[i+3:]
	}
}

func parseStmts(toks []mTok) []mStmt {
	st, _ := parseBlock(toks, 0)
	return st
}

func parseBlock(toks []mTok, i int) ([]mStmt, int) {
	var out []mStmt
	for i < len(toks) {
		switch toks[i].kind {
		case "close":
			return out, i + 1
		case "open": // a bare { … } block
			body, j := parseBlock(toks, i+1)
			out = append(out, body...)
			i = j
			continue
		}
		s, j := parseStmt(toks, i)
		out = append(out, s)
		i = j
	}
	return out, i
}

func hasWord(text, w string) bool {
	l := strings.ToLower(text)
	return l == w || strings.HasPrefix(l, w+" ") || strings.HasPrefix(l, w+"(")
}

// parseStmt parses one statement at toks[i]: a command, a while loop, or an if with its
// elseif/else chain.
func parseStmt(toks []mTok, i int) (mStmt, int) {
	t := toks[i].text
	switch {
	case hasWord(t, "while"):
		return parseCond("while", toks, i)
	case hasWord(t, "if"):
		s, j := parseCond("if", toks, i)
		for j < len(toks) && toks[j].kind == "cmd" {
			l := toks[j].text
			if hasWord(l, "elseif") {
				e, k := parseCond("elseif", toks, j)
				s.elifs = append(s.elifs, e)
				j = k
				continue
			}
			if !hasWord(l, "else") {
				break
			}
			rest := strings.TrimSpace(l[4:])
			var body []mStmt
			if rest != "" { // "else cmd" or "else if (…) …"
				sub := append([]mTok{{"cmd", rest}}, toks[j+1:]...)
				st, k := parseStmt(sub, 0)
				body, j = []mStmt{st}, j+k
			} else if j+1 < len(toks) && toks[j+1].kind == "open" {
				body, j = parseBlock(toks, j+2)
			} else {
				j++
			}
			s.elifs = append(s.elifs, mStmt{kind: "else", body: body})
			break
		}
		return s, j
	}
	return mStmt{kind: "cmd", text: t}, i + 1
}

// parseCond parses "if (cond) cmd" / "if (cond) { … }" (also while, elseif).
func parseCond(kind string, toks []mTok, i int) (mStmt, int) {
	t := strings.TrimSpace(toks[i].text[len(kind):])
	cond, rest := splitCond(t)
	s := mStmt{kind: kind, text: cond}
	if kind == "elseif" {
		s.kind = "elseif"
	}
	if rest != "" {
		s.body = []mStmt{{kind: "cmd", text: rest}}
		return s, i + 1
	}
	if i+1 < len(toks) && toks[i+1].kind == "open" {
		body, j := parseBlock(toks, i+2)
		s.body = body
		return s, j
	}
	return s, i + 1
}

// splitCond takes "(a == b) rest" and returns "a == b", "rest".
func splitCond(t string) (string, string) {
	t = strings.TrimSpace(t)
	if !strings.HasPrefix(t, "(") {
		return t, ""
	}
	depth := 0
	for i, r := range t {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return strings.TrimSpace(t[1:i]), strings.TrimSpace(t[i+1:])
			}
		}
	}
	return strings.TrimSpace(t[1:]), ""
}

// ---------- execution context ----------

type mCtx struct {
	e       *mircEngine
	c       *Conn
	nick    string
	address string
	chan_   string
	target  string
	params  []string // $1, $2, …
	extra   map[string]string
	locals  map[string]string
	halted  bool
	missing bool // a $$identifier was empty
	haltdef bool
	ret     *string
	steps   int
	depth   int
}

var errHalt = errors.New("halt")
var errReturn = errors.New("return")
var errBudget = errors.New("script ran too long (stopped)")

const mircMaxSteps = 200000

func (e *mircEngine) newCtx(c *Conn) *mCtx {
	return &mCtx{e: e, c: c, extra: map[string]string{}, locals: map[string]string{}}
}

func (x *mCtx) run(body []mStmt) error {
	for _, s := range body {
		x.steps++
		if x.steps > mircMaxSteps {
			return errBudget
		}
		switch s.kind {
		case "cmd":
			if err := x.command(s.text); err != nil {
				return err
			}
		case "if":
			taken := false
			if x.cond(s.text) {
				taken = true
				if err := x.run(s.body); err != nil {
					return err
				}
			}
			for _, b := range s.elifs {
				if taken {
					break
				}
				if b.kind == "else" || x.cond(b.text) {
					taken = true
					if err := x.run(b.body); err != nil {
						return err
					}
				}
			}
		case "while":
			for x.cond(s.text) {
				x.steps++
				if x.steps > mircMaxSteps {
					return errBudget
				}
				err := x.run(s.body)
				if err == errBreak {
					break
				}
				if err == errContinue {
					continue
				}
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

var errBreak = errors.New("break")
var errContinue = errors.New("continue")

// ---------- evaluation ----------

// eval evaluates identifiers ($…) and variables (%…) in a line, mIRC style: they're
// space-separated words, and $+ joins its neighbours.
func (x *mCtx) eval(s string) string {
	var parts []string
	plus := false
	i := 0
	for i < len(s) {
		if s[i] == ' ' {
			i++
			continue
		}
		start := i
		var word string
		if s[i] == '$' || s[i] == '%' {
			val, end := x.evalIdent(s, i)
			word = val + s[end:wordEnd(s, end)]
			i = wordEnd(s, end)
			if s[start:i] == "$+" {
				plus = true
				continue
			}
		} else {
			i = wordEnd(s, i)
			word = s[start:i]
		}
		if plus && len(parts) > 0 {
			parts[len(parts)-1] += word
			plus = false
		} else {
			parts = append(parts, word)
		}
	}
	return strings.Join(parts, " ")
}

func wordEnd(s string, i int) int {
	for i < len(s) && s[i] != ' ' {
		i++
	}
	return i
}

// evalIdent evaluates one $identifier(…) or %variable at s[i] and returns where it ended.
func (x *mCtx) evalIdent(s string, i int) (string, int) {
	if s[i] == '%' {
		j := i + 1
		for j < len(s) && (isIdentChar(rune(s[j])) || s[j] == '.') {
			j++
		}
		name := strings.ToLower(s[i:j])
		if name == "%" {
			return "%", j
		}
		return x.getVar(name), j
	}
	j := i + 1
	if j+1 < len(s) && s[j] == '+' && s[j+1] == '(' { // $+(a,b,c) joins its arguments
		end := matchParen(s, j+1)
		var b strings.Builder
		for _, p := range splitArgs(s[j+2 : end]) {
			b.WriteString(x.eval(p))
		}
		return b.String(), end + 1
	}
	if j < len(s) && s[j] == '+' {
		return "", j + 1 // $+ (handled by eval)
	}
	if j < len(s) && s[j] == '!' {
		return "$", j + 1 // $!nick: evaluated later (timers)
	}
	if j < len(s) && s[j] == '$' { // $$1: required; the command doesn't run without it
		v, end := x.evalIdent(s, j)
		if v == "" {
			x.missing = true
		}
		return v, end
	}
	for j < len(s) && (isIdentChar(rune(s[j])) || (s[j] == '-' && j > i+1 && isDigits(s[i+1:j]))) {
		j++
	}
	name := strings.ToLower(s[i+1 : j])
	var args []string
	hasArgs := false
	if j < len(s) && s[j] == '(' {
		end := matchParen(s, j)
		args = splitArgs(s[j+1 : end])
		for k := range args {
			args[k] = x.eval(args[k])
		}
		hasArgs = true
		j = end + 1
	}
	prop := ""
	if j < len(s) && s[j] == '.' && j+1 < len(s) && unicode.IsLetter(rune(s[j+1])) {
		k := j + 1
		for k < len(s) && unicode.IsLetter(rune(s[k])) {
			k++
		}
		prop, j = strings.ToLower(s[j+1:k]), k
	}
	return x.ident(name, args, hasArgs, prop), j
}

func isIdentChar(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func matchParen(s string, open int) int {
	depth := 0
	for k := open; k < len(s); k++ {
		switch s[k] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return len(s) - 1
}

// splitArgs splits "a, b, $f(c, d)" at top-level commas.
func splitArgs(s string) []string {
	var out []string
	depth, last := 0, 0
	for k := 0; k < len(s); k++ {
		switch s[k] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[last:k]))
				last = k + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[last:]))
}

func (x *mCtx) getVar(name string) string {
	if v, ok := x.locals[name]; ok {
		return v
	}
	return x.e.vars[name]
}

func arg(a []string, i int) string {
	if i < len(a) {
		return a[i]
	}
	return ""
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func mFmtNum(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// tokens for $gettok & co: c is an ASCII code
func toks(text, c string) []string {
	sep := string(rune(atoi(c)))
	if atoi(c) == 0 {
		sep = " "
	}
	var out []string
	for _, t := range strings.Split(text, sep) {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// tokRange: "3" → [3,3], "2-" → [2,end], "2-4" → [2,4], "-1" → last
func tokRange(spec string, n int) (int, int) {
	if strings.HasPrefix(spec, "-") && isDigits(spec[1:]) {
		k := n + 1 - atoi(spec[1:])
		return k, k
	}
	a, b, rng := strings.Cut(spec, "-")
	from := atoi(a)
	to := from
	if rng {
		to = n
		if b != "" {
			to = atoi(b)
		}
	}
	return from, to
}

func (x *mCtx) paramRange(from, to int) string {
	if from < 1 {
		from = 1
	}
	if to > len(x.params) {
		to = len(x.params)
	}
	if from > to {
		return ""
	}
	return strings.Join(x.params[from-1:to], " ")
}

func (x *mCtx) ident(name string, a []string, hasArgs bool, prop string) string {
	// $1 $2- $3-5
	if name != "" && unicode.IsDigit(rune(name[0])) {
		if name == "0" {
			return strconv.Itoa(len(x.params))
		}
		from, to := tokRange(name, len(x.params))
		return x.paramRange(from, to)
	}
	c := x.c
	switch name {
	case "nick":
		if hasArgs && c != nil { // $nick(#chan, N)
			users := c.Users(arg(a, 0))
			sort.Slice(users, func(i, j int) bool { return lower(users[i].Nick) < lower(users[j].Nick) })
			n := atoi(arg(a, 1))
			if arg(a, 1) == "0" {
				return strconv.Itoa(len(users))
			}
			if n >= 1 && n <= len(users) {
				return users[n-1].Nick
			}
			return ""
		}
		return x.nick
	case "chan":
		return x.chan_
	case "target":
		return x.target
	case "me":
		if c != nil {
			return c.Nick()
		}
	case "network":
		if c != nil {
			if c.network != "" {
				return c.network
			}
			return c.cfg.Name
		}
	case "server":
		if c != nil {
			return c.cfg.Host
		}
	case "port":
		if c != nil {
			return strconv.Itoa(c.cfg.Port)
		}
	case "address", "mask":
		if hasArgs { // $address(nick, type) / $mask(nick!user@host, type): 2 = *!*@host, 5 = full
			full := arg(a, 0)
			if name == "address" {
				if c == nil {
					return ""
				}
				uh := c.userHost(arg(a, 0))
				if uh == "" {
					return ""
				}
				full = arg(a, 0) + "!" + uh
			}
			n, rest, _ := strings.Cut(full, "!")
			u, h, _ := strings.Cut(rest, "@")
			switch atoi(arg(a, 1)) {
			case 1:
				return "*!*" + strings.TrimLeft(u, "~") + "@" + h
			case 2:
				return "*!*@" + h
			case 3:
				return "*!*" + strings.TrimLeft(u, "~") + "@" + h
			case 0, 5:
				return n + "!" + u + "@" + h
			default:
				return "*!*@" + h
			}
		}
		return x.address
	case "fulladdress":
		return x.nick + "!" + x.address
	case "site":
		_, h, _ := strings.Cut(x.address, "@")
		return h
	case "knick", "newnick", "opnick", "snicks":
		return x.extra[name]
	case "active":
		return x.target
	case "null":
		return ""
	case "true":
		return "$true"
	case "false":
		return "$false"
	case "cr":
		return "\r"
	case "lf", "crlf":
		return "\n"
	case "version":
		return appVersion
	case "time":
		if hasArgs {
			return mircTimeFormat(time.Now(), arg(a, 0))
		}
		return time.Now().Format("15:04:05")
	case "date":
		return time.Now().Format("02/01/2006")
	case "ctime":
		return strconv.FormatInt(time.Now().Unix(), 10)
	case "ticks":
		return strconv.FormatInt(time.Now().UnixMilli(), 10)
	case "asctime":
		t := time.Now()
		if len(a) > 0 && a[0] != "" {
			t = time.Unix(int64(atoi(a[0])), 0)
		}
		if len(a) > 1 {
			return mircTimeFormat(t, a[1])
		}
		return t.Format("Mon Jan 02 15:04:05 2006")
	case "duration":
		return mircDuration(atoi(arg(a, 0)))
	case "rand":
		lo, hi := atoi(arg(a, 0)), atoi(arg(a, 1))
		if hi < lo {
			lo, hi = hi, lo
		}
		return strconv.Itoa(lo + rand.Intn(hi-lo+1))
	case "len":
		return strconv.Itoa(len([]rune(arg(a, 0))))
	case "upper":
		return strings.ToUpper(arg(a, 0))
	case "lower":
		return strings.ToLower(arg(a, 0))
	case "left", "right":
		r := []rune(arg(a, 0))
		n := atoi(arg(a, 1))
		if n < 0 {
			n = len(r) + n
		}
		if n < 0 {
			n = 0
		}
		if n > len(r) {
			n = len(r)
		}
		if name == "left" {
			return string(r[:n])
		}
		return string(r[len(r)-n:])
	case "mid":
		r := []rune(arg(a, 0))
		s := atoi(arg(a, 1))
		if s < 1 {
			s = 1
		}
		if s > len(r) {
			return ""
		}
		n := len(r) - s + 1
		if len(a) > 2 {
			n = atoi(a[2])
		}
		if s-1+n > len(r) {
			n = len(r) - s + 1
		}
		if n < 0 {
			return ""
		}
		return string(r[s-1 : s-1+n])
	case "replace", "replacecs":
		out := arg(a, 0)
		for k := 1; k+1 < len(a); k += 2 {
			if name == "replace" {
				out = replaceFold(out, a[k], a[k+1])
			} else {
				out = strings.ReplaceAll(out, a[k], a[k+1])
			}
		}
		return out
	case "remove":
		out := arg(a, 0)
		for _, r := range a[1:] {
			out = replaceFold(out, r, "")
		}
		return out
	case "pos":
		idx := strings.Index(strings.ToLower(arg(a, 0)), strings.ToLower(arg(a, 1)))
		if idx < 0 {
			return ""
		}
		return strconv.Itoa(len([]rune(arg(a, 0)[:idx])) + 1)
	case "count":
		if arg(a, 1) == "" {
			return "0"
		}
		return strconv.Itoa(strings.Count(strings.ToLower(arg(a, 0)), strings.ToLower(arg(a, 1))))
	case "str":
		n := atoi(arg(a, 1))
		if n < 0 || n > 1000 {
			n = 0
		}
		return strings.Repeat(arg(a, 0), n)
	case "strip":
		return stripFormatting(arg(a, 0))
	case "chr":
		return string(rune(atoi(arg(a, 0))))
	case "asc":
		r := []rune(arg(a, 0))
		if len(r) == 0 {
			return "0"
		}
		return strconv.Itoa(int(r[0]))
	case "calc":
		v, err := calc(arg(a, 0))
		if err != nil {
			return "0"
		}
		return mFmtNum(v)
	case "int":
		return strconv.Itoa(int(atof(arg(a, 0))))
	case "abs":
		return mFmtNum(math.Abs(atof(arg(a, 0))))
	case "round":
		p := math.Pow(10, float64(atoi(arg(a, 1))))
		return strconv.FormatFloat(math.Round(atof(arg(a, 0))*p)/p, 'f', atoi(arg(a, 1)), 64)
	case "iif":
		if x.cond(arg(a, 0)) {
			return arg(a, 1)
		}
		return arg(a, 2)
	case "gettok":
		t := toks(arg(a, 0), arg(a, 2))
		if arg(a, 1) == "0" {
			return strconv.Itoa(len(t))
		}
		from, to := tokRange(arg(a, 1), len(t))
		if from < 1 || from > len(t) {
			return ""
		}
		if to > len(t) {
			to = len(t)
		}
		sep := string(rune(atoi(arg(a, 2))))
		return strings.Join(t[from-1:to], sep)
	case "numtok":
		return strconv.Itoa(len(toks(arg(a, 0), arg(a, 1))))
	case "istok":
		for _, t := range toks(arg(a, 0), arg(a, 2)) {
			if strings.EqualFold(t, arg(a, 1)) {
				return "$true"
			}
		}
		return "$false"
	case "findtok":
		for k, t := range toks(arg(a, 0), arg(a, 3)) {
			if strings.EqualFold(t, arg(a, 1)) {
				return strconv.Itoa(k + 1)
			}
		}
		return ""
	case "addtok":
		t := toks(arg(a, 0), arg(a, 2))
		for _, v := range t {
			if strings.EqualFold(v, arg(a, 1)) {
				return arg(a, 0)
			}
		}
		return strings.Join(append(t, arg(a, 1)), string(rune(atoi(arg(a, 2)))))
	case "deltok":
		t := toks(arg(a, 0), arg(a, 2))
		from, to := tokRange(arg(a, 1), len(t))
		if from < 1 || from > len(t) {
			return arg(a, 0)
		}
		if to > len(t) {
			to = len(t)
		}
		return strings.Join(append(append([]string{}, t[:from-1]...), t[to:]...), string(rune(atoi(arg(a, 2)))))
	case "remtok":
		var keep []string
		for _, v := range toks(arg(a, 0), arg(a, 2)) {
			if !strings.EqualFold(v, arg(a, 1)) {
				keep = append(keep, v)
			}
		}
		return strings.Join(keep, string(rune(atoi(arg(a, 2)))))
	case "hget":
		t := x.e.hash[strings.ToLower(arg(a, 0))]
		if len(a) < 2 {
			if t == nil {
				return ""
			}
			return arg(a, 0)
		}
		if t == nil {
			return ""
		}
		return t[strings.ToLower(arg(a, 1))]
	case "var":
		return x.getVar(strings.ToLower(arg(a, 0)))
	case "comchan":
		if c == nil {
			return ""
		}
		var chans []string
		for _, ch := range c.Channels() {
			for _, u := range c.Users(ch) {
				if strings.EqualFold(u.Nick, arg(a, 0)) {
					chans = append(chans, ch)
				}
			}
		}
		if arg(a, 1) == "0" {
			return strconv.Itoa(len(chans))
		}
		if n := atoi(arg(a, 1)); n >= 1 && n <= len(chans) {
			return chans[n-1]
		}
		return ""
	case "chan_count", "chans":
		if c != nil {
			return strconv.Itoa(len(c.Channels()))
		}
	}
	// custom alias used as an identifier: $myalias(a, b) → its /return value
	if al := x.e.aliases[name]; al != nil && x.depth < 50 {
		sub := x.e.newCtx(c)
		sub.nick, sub.address, sub.chan_, sub.target, sub.extra = x.nick, x.address, x.chan_, x.target, x.extra
		sub.depth = x.depth + 1
		sub.params = a
		var r string
		sub.ret = &r
		sub.run(al.body)
		x.steps += sub.steps
		return r
	}
	return ""
}

func replaceFold(s, old, new string) string {
	if old == "" {
		return s
	}
	re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(old))
	return re.ReplaceAllLiteralString(s, new)
}

func mircDuration(secs int) string {
	if secs <= 0 {
		return "0secs"
	}
	parts := []struct {
		n    int
		unit string
	}{{secs / 604800, "wk"}, {secs % 604800 / 86400, "day"}, {secs % 86400 / 3600, "hr"}, {secs % 3600 / 60, "min"}, {secs % 60, "sec"}}
	var out []string
	for _, p := range parts {
		if p.n > 0 {
			u := p.unit
			if p.n != 1 {
				u += "s"
			}
			out = append(out, strconv.Itoa(p.n)+u)
		}
	}
	return strings.Join(out, " ")
}

// mircTimeFormat handles the common mIRC time tokens: yyyy yy mmmm mmm mm m dddd ddd dd d HH H hh h nn ss tt.
func mircTimeFormat(t time.Time, f string) string {
	rep := []struct{ k, v string }{
		{"yyyy", t.Format("2006")}, {"yy", t.Format("06")}, {"mmmm", t.Format("January")}, {"mmm", t.Format("Jan")},
		{"mm", t.Format("01")}, {"dddd", t.Format("Monday")}, {"ddd", t.Format("Mon")}, {"dd", t.Format("02")},
		{"HH", t.Format("15")}, {"hh", t.Format("03")}, {"nn", t.Format("04")}, {"ss", t.Format("05")}, {"tt", t.Format("pm")},
		{"H", strconv.Itoa(t.Hour())}, {"h", t.Format("3")}, {"d", strconv.Itoa(t.Day())}, {"m", strconv.Itoa(int(t.Month()))},
	}
	var b strings.Builder
	for i := 0; i < len(f); {
		matched := false
		for _, r := range rep {
			if strings.HasPrefix(f[i:], r.k) {
				b.WriteString(r.v)
				i += len(r.k)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(f[i])
			i++
		}
	}
	return b.String()
}

// ---------- conditions ----------

func (x *mCtx) cond(s string) bool {
	s = strings.TrimSpace(s)
	if strings.Contains(s, " ]") {
		s = x.resolveBrackets(s)
	}
	if s == "" {
		return false
	}
	// || and && at the top level (|| binds loosest)
	if parts := splitTop(s, "||"); len(parts) > 1 {
		for _, p := range parts {
			if x.cond(p) {
				return true
			}
		}
		return false
	}
	if parts := splitTop(s, "&&"); len(parts) > 1 {
		for _, p := range parts {
			if !x.cond(p) {
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(s, "!(") && matchParen(s, 1) == len(s)-1 {
		return !x.cond(s[2 : len(s)-1])
	}
	if strings.HasPrefix(s, "(") && matchParen(s, 0) == len(s)-1 {
		return x.cond(s[1 : len(s)-1])
	}
	words := strings.Fields(s)
	depth := 0
	for i, w := range words {
		d := depth
		depth += strings.Count(w, "(") - strings.Count(w, ")")
		if i == 0 || d != 0 {
			continue
		}
		op := strings.ToLower(w)
		neg := false
		if strings.HasPrefix(op, "!") && len(op) > 1 && isOperator(op[1:]) {
			neg, op = true, op[1:]
		}
		if !isOperator(op) {
			continue
		}
		l := x.eval(strings.Join(words[:i], " "))
		r := x.eval(strings.Join(words[i+1:], " "))
		return x.compare(l, op, r) != neg
	}
	// a single value: true unless empty, 0 or $false
	neg := false
	if strings.HasPrefix(s, "!") {
		neg, s = true, s[1:]
	}
	v := x.eval(s)
	ok := v != "" && v != "0" && v != "$false"
	return ok != neg
}

var mircOperators = map[string]bool{"==": true, "===": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
	"isin": true, "isincs": true, "iswm": true, "iswmcs": true, "isnum": true, "isletter": true, "isalnum": true,
	"isalpha": true, "islower": true, "isupper": true, "ison": true, "isop": true, "isvoice": true, "ishop": true,
	"isreg": true, "ischan": true, "//": true, "\\\\": true}

func isOperator(op string) bool { return mircOperators[op] }

// splitTop splits s on sep outside parentheses.
func splitTop(s, sep string) []string {
	var out []string
	depth, last := 0, 0
	for k := 0; k < len(s); k++ {
		switch s[k] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(s[k:], sep) {
			out = append(out, strings.TrimSpace(s[last:k]))
			k += len(sep) - 1
			last = k + 1
		}
	}
	return append(out, strings.TrimSpace(s[last:]))
}

func (x *mCtx) compare(l, op, r string) bool {
	isNum := func(s string) bool {
		_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return err == nil && s != ""
	}
	switch op {
	case "==":
		if isNum(l) && isNum(r) {
			return atof(l) == atof(r)
		}
		return strings.EqualFold(l, r)
	case "===":
		return l == r
	case "!=":
		if isNum(l) && isNum(r) {
			return atof(l) != atof(r)
		}
		return !strings.EqualFold(l, r)
	case "<", ">", "<=", ">=":
		var c int
		if isNum(l) && isNum(r) {
			a, b := atof(l), atof(r)
			c = map[bool]int{true: -1, false: 0}[a < b]
			if a > b {
				c = 1
			}
		} else {
			c = strings.Compare(strings.ToLower(l), strings.ToLower(r))
		}
		return map[string]bool{"<": c < 0, ">": c > 0, "<=": c <= 0, ">=": c >= 0}[op]
	case "//":
		return atof(r) != 0 && math.Mod(atof(l), atof(r)) == 0
	case "\\\\":
		return atof(r) == 0 || math.Mod(atof(l), atof(r)) != 0
	case "isin":
		return strings.Contains(strings.ToLower(r), strings.ToLower(l))
	case "isincs":
		return strings.Contains(r, l)
	case "iswm":
		return wildMatch(strings.ToLower(r), strings.ToLower(l))
	case "iswmcs":
		return wildMatch(r, l)
	case "isnum":
		if !isNum(l) {
			return false
		}
		if r == "" {
			return true
		}
		lo, hi, _ := strings.Cut(r, "-")
		v := atof(l)
		return (lo == "" || v >= atof(lo)) && (hi == "" || v <= atof(hi))
	case "isletter":
		return l != "" && strings.IndexFunc(l, func(c rune) bool { return !unicode.IsLetter(c) }) < 0 && (r == "" || strings.ContainsAny(r, l))
	case "isalpha":
		return l != "" && strings.IndexFunc(l, func(c rune) bool { return !unicode.IsLetter(c) }) < 0
	case "isalnum":
		return l != "" && strings.IndexFunc(l, func(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsDigit(c) }) < 0
	case "islower":
		return l == strings.ToLower(l)
	case "isupper":
		return l == strings.ToUpper(l)
	case "ischan":
		return x.c != nil && x.c.isChannel(l)
	case "ison", "isop", "isvoice", "ishop", "isreg":
		if x.c == nil {
			return false
		}
		for _, u := range x.c.Users(r) {
			if strings.EqualFold(u.Nick, l) {
				switch op {
				case "ison":
					return true
				case "isop":
					return strings.ContainsAny(u.Prefix, "~&@")
				case "ishop":
					return strings.Contains(u.Prefix, "%")
				case "isvoice":
					return strings.Contains(u.Prefix, "+")
				case "isreg":
					return u.Prefix == ""
				}
			}
		}
	}
	return false
}

// wildMatch: * any, ? one char, & one word.
func wildMatch(text, pattern string) bool {
	var b strings.Builder
	b.WriteString("(?s)^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '&':
			b.WriteString(`\S+`)
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(text)
}

// calc evaluates + - * / % ^ and parentheses.
func calc(s string) (float64, error) {
	p := &calcParser{s: strings.ReplaceAll(s, " ", "")}
	v := p.expr()
	if p.err != nil || p.i != len(p.s) {
		return 0, errors.New("bad expression")
	}
	return v, nil
}

type calcParser struct {
	s   string
	i   int
	err error
}

func (p *calcParser) expr() float64 {
	v := p.term()
	for p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		op := p.s[p.i]
		p.i++
		if op == '+' {
			v += p.term()
		} else {
			v -= p.term()
		}
	}
	return v
}

func (p *calcParser) term() float64 {
	v := p.power()
	for p.i < len(p.s) && strings.IndexByte("*/%", p.s[p.i]) >= 0 {
		op := p.s[p.i]
		p.i++
		r := p.power()
		switch op {
		case '*':
			v *= r
		case '/':
			if r == 0 {
				return 0
			}
			v /= r
		case '%':
			if r == 0 {
				return 0
			}
			v = math.Mod(v, r)
		}
	}
	return v
}

func (p *calcParser) power() float64 {
	v := p.unary()
	if p.i < len(p.s) && p.s[p.i] == '^' {
		p.i++
		v = math.Pow(v, p.power())
	}
	return v
}

func (p *calcParser) unary() float64 {
	if p.i < len(p.s) && p.s[p.i] == '-' {
		p.i++
		return -p.unary()
	}
	if p.i < len(p.s) && p.s[p.i] == '(' {
		p.i++
		v := p.expr()
		if p.i < len(p.s) && p.s[p.i] == ')' {
			p.i++
		} else {
			p.err = errors.New("missing )")
		}
		return v
	}
	start := p.i
	for p.i < len(p.s) && (p.s[p.i] >= '0' && p.s[p.i] <= '9' || p.s[p.i] == '.') {
		p.i++
	}
	f, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		p.err = err
	}
	return f
}

// ---------- commands ----------

var mircBracket = regexp.MustCompile(`(\S*)\s+\[ ((?:[^\[\]]|\S\[|\]\S)*?) \](\s|$)`)

// resolveBrackets evaluates mIRC's [ … ] evaluation brackets, innermost first:
// %count. [ $+ [ $nick ] ] → %count.bob
func (x *mCtx) resolveBrackets(line string) string {
	for n := 0; n < 20 && strings.Contains(line, " ]"); n++ {
		loc := mircBracket.FindStringSubmatchIndex(line)
		if loc == nil {
			break
		}
		prev, inner := line[loc[2]:loc[3]], line[loc[4]:loc[5]]
		joined := strings.HasPrefix(strings.TrimSpace(inner), "$+")
		val := x.eval(strings.TrimPrefix(strings.TrimSpace(inner), "$+"))
		sep := " "
		if joined || prev == "" {
			sep = ""
		}
		if prev == "" && loc[2] > 0 {
			sep = ""
		}
		line = line[:loc[0]] + prev + sep + val + line[loc[6]:]
	}
	return line
}

func (x *mCtx) command(line string) error {
	line = strings.TrimSpace(line)
	if strings.Contains(line, " ]") {
		line = x.resolveBrackets(line)
	}
	// %var = value
	if strings.HasPrefix(line, "%") {
		if name, val, ok := strings.Cut(line, " = "); ok && !strings.Contains(name, " ") {
			x.setVar(strings.ToLower(name), x.evalAssign(val), false)
			return nil
		}
	}
	silent := false
	for strings.HasPrefix(line, "/") || strings.HasPrefix(line, ".") {
		if line[0] == '.' {
			silent = true
		}
		line = line[1:]
	}
	_ = silent
	word, rest, _ := strings.Cut(line, " ")
	cmd := strings.ToLower(word)
	rest = strings.TrimSpace(rest)

	// commands that take unevaluated variable names
	switch cmd {
	case "var":
		for _, part := range splitArgs(rest) {
			part = strings.TrimSpace(part)
			flags := ""
			if strings.HasPrefix(part, "-") {
				flags, part, _ = strings.Cut(part, " ")
				_ = flags
			}
			name, val, hasVal := strings.Cut(part, " = ")
			if !hasVal {
				name, val, _ = strings.Cut(part, " ")
			}
			name = strings.ToLower(strings.TrimSpace(name))
			if strings.HasPrefix(name, "%") {
				x.locals[name] = x.evalAssign(val)
			}
		}
		return nil
	case "set":
		for strings.HasPrefix(rest, "-") { // -u, -e, -n…: flags ignored
			_, rest, _ = strings.Cut(rest, " ")
		}
		name, val, _ := strings.Cut(rest, " ")
		x.setVar(strings.ToLower(name), x.eval(val), true)
		return nil
	case "unset":
		for _, n := range strings.Fields(rest) {
			n = strings.ToLower(n)
			if strings.HasSuffix(n, "*") {
				for k := range x.e.vars {
					if strings.HasPrefix(k, strings.TrimSuffix(n, "*")) {
						delete(x.e.vars, k)
					}
				}
			}
			delete(x.e.vars, n)
			delete(x.locals, n)
		}
		x.e.saveVarsSoon()
		return nil
	case "inc", "dec":
		for strings.HasPrefix(rest, "-") {
			_, rest, _ = strings.Cut(rest, " ")
		}
		name, by, _ := strings.Cut(rest, " ")
		name = strings.ToLower(name)
		n := 1.0
		if strings.TrimSpace(by) != "" {
			n = atof(x.eval(by))
		}
		if cmd == "dec" {
			n = -n
		}
		_, local := x.locals[name]
		x.setVar(name, mFmtNum(atof(x.getVar(name))+n), !local)
		return nil
	case "return":
		if x.ret != nil {
			*x.ret = x.eval(rest)
		}
		return errReturn
	case "halt":
		x.halted = true
		return errHalt
	case "haltdef":
		x.haltdef = true
		return nil
	case "break":
		return errBreak
	case "continue":
		return errContinue
	}

	x.missing = false
	rest = x.eval(rest)
	if x.missing {
		return errHalt
	}
	c := x.c
	cur := x.target
	if x.chan_ != "" {
		cur = x.chan_
	}
	switch {
	case cmd == "echo":
		target := ""
		for strings.HasPrefix(rest, "-") {
			_, rest, _ = strings.Cut(rest, " ")
		}
		if f, r, ok := strings.Cut(rest, " "); ok && c != nil && c.isChannel(f) {
			target, rest = f, r
		}
		if c != nil {
			if target == "" {
				target = "*active*"
			}
			c.info(target, "script", rest)
		}
	case cmd == "msg" || cmd == "privmsg":
		t, text, _ := strings.Cut(rest, " ")
		if c != nil && t != "" && text != "" {
			for _, tg := range strings.Split(t, ",") {
				c.Say(tg, text, true)
			}
		}
	case cmd == "say":
		if c != nil && cur != "" && rest != "" {
			c.Say(cur, rest, true)
		}
	case cmd == "notice":
		t, text, _ := strings.Cut(rest, " ")
		if c != nil && t != "" && text != "" {
			c.Notice(t, text)
		}
	case cmd == "describe":
		t, text, _ := strings.Cut(rest, " ")
		if c != nil && t != "" {
			c.Action(t, text)
		}
	case cmd == "me" || cmd == "action":
		if c != nil && cur != "" {
			c.Action(cur, rest)
		}
	case cmd == "hmake":
		name, _, _ := strings.Cut(rest, " ")
		if x.e.hash[strings.ToLower(name)] == nil {
			x.e.hash[strings.ToLower(name)] = map[string]string{}
		}
	case cmd == "hadd":
		for strings.HasPrefix(rest, "-") {
			_, rest, _ = strings.Cut(rest, " ")
		}
		f := strings.SplitN(rest, " ", 3)
		if len(f) >= 2 {
			t := strings.ToLower(f[0])
			if x.e.hash[t] == nil {
				x.e.hash[t] = map[string]string{}
			}
			x.e.hash[t][strings.ToLower(f[1])] = arg(f, 2)
		}
	case cmd == "hinc", cmd == "hdec":
		f := strings.Fields(rest)
		if len(f) >= 2 {
			t := strings.ToLower(f[0])
			if x.e.hash[t] == nil {
				x.e.hash[t] = map[string]string{}
			}
			n := 1.0
			if len(f) > 2 {
				n = atof(f[2])
			}
			if cmd == "hdec" {
				n = -n
			}
			k := strings.ToLower(f[1])
			x.e.hash[t][k] = mFmtNum(atof(x.e.hash[t][k]) + n)
		}
	case cmd == "hdel":
		f := strings.Fields(rest)
		if len(f) >= 2 && x.e.hash[strings.ToLower(f[0])] != nil {
			delete(x.e.hash[strings.ToLower(f[0])], strings.ToLower(f[1]))
		}
	case cmd == "hfree":
		delete(x.e.hash, strings.ToLower(strings.TrimSpace(rest)))
	case cmd == "timers" || strings.HasPrefix(cmd, "timer"):
		x.timerCmd(cmd, rest)
	default:
		if al := x.e.aliases[cmd]; al != nil {
			if x.depth >= 50 {
				return errors.New("aliases calling each other too deeply")
			}
			sub := x.e.newCtx(c)
			sub.nick, sub.address, sub.chan_, sub.target, sub.extra = x.nick, x.address, x.chan_, x.target, x.extra
			sub.depth = x.depth + 1
			sub.params = strings.Fields(rest)
			err := sub.run(al.body)
			x.steps += sub.steps
			if err == errHalt {
				x.halted = true
				return errHalt
			}
			if err != nil && err != errReturn {
				return err
			}
			return nil
		}
		if c == nil {
			return nil
		}
		switch cmd {
		case "reload", "run", "exit", "load", "unload", "write", "remove", "rename", "copy", "mkdir", "dll", "sockopen", "sockwrite", "sockclose", "dialog", "window", "play", "splay", "savebuf", "loadbuf", "fopen", "fwrite", "flushini", "writeini", "remini":
			return nil // not available to scripts here (files, programs, sockets)
		}
		// everything else is a normal IRChatterbox / IRC command
		line := "/" + word
		if rest != "" {
			line += " " + rest
		}
		buf := cur
		if buf == "" {
			buf = x.target
		}
		x.e.p.app.runCommandFrom(c, buf, line, true)
	}
	return nil
}

// evalAssign: "%x = $calc(…)" or "%x = 1 + 2" (mIRC does maths on simple var assignments)
func (x *mCtx) evalAssign(val string) string {
	v := x.eval(strings.TrimSpace(val))
	if f := strings.Fields(v); len(f) == 3 && strings.Contains("+-*/%^", f[1]) && len(f[1]) == 1 {
		if r, err := calc(v); err == nil {
			return mFmtNum(r)
		}
	}
	return v
}

func (x *mCtx) setVar(name, val string, global bool) {
	if !strings.HasPrefix(name, "%") {
		return
	}
	if _, local := x.locals[name]; local && !global {
		x.locals[name] = val
		return
	}
	if _, local := x.locals[name]; local {
		x.locals[name] = val
		return
	}
	x.e.vars[name] = val
	x.e.saveVarsSoon()
}

// /timer[name] [-m] reps delay command  ·  /timer[name] off  ·  /timers off
func (x *mCtx) timerCmd(cmd, rest string) {
	e := x.e
	if cmd == "timers" {
		if strings.EqualFold(strings.TrimSpace(rest), "off") {
			e.stopTimersLocked()
		}
		return
	}
	name := strings.TrimPrefix(cmd, "timer")
	f := strings.Fields(rest)
	if len(f) == 1 && strings.EqualFold(f[0], "off") {
		if t := e.timers[strings.ToLower(name)]; t != nil {
			close(t.stop)
			delete(e.timers, strings.ToLower(name))
		}
		return
	}
	ms := false
	for len(f) > 0 && strings.HasPrefix(f[0], "-") {
		if strings.Contains(f[0], "m") {
			ms = true
		}
		f = f[1:]
	}
	if len(f) < 3 {
		return
	}
	reps, delay := atoi(f[0]), atof(f[1])
	unit := time.Second
	if ms {
		unit = time.Millisecond
	}
	d := time.Duration(delay * float64(unit))
	if d < 50*time.Millisecond {
		d = 50 * time.Millisecond
	}
	if name == "" {
		e.timerN++
		name = strconv.Itoa(e.timerN)
	}
	key := strings.ToLower(name)
	if old := e.timers[key]; old != nil {
		close(old.stop)
	}
	ctx := &mCtx{e: e, c: x.c, nick: x.nick, address: x.address, chan_: x.chan_, target: x.target, params: x.params, extra: x.extra, locals: map[string]string{}}
	t := &mircTimer{name: key, stop: make(chan struct{}), conn: x.c, reps: reps, delay: d, cmd: strings.Join(f[2:], " "), ctx: ctx}
	e.timers[key] = t
	go e.runTimer(t)
}

func (e *mircEngine) runTimer(t *mircTimer) {
	n := 0
	tick := time.NewTicker(t.delay)
	defer tick.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-tick.C:
		}
		e.mu.Lock()
		if e.timers[t.name] != t {
			e.mu.Unlock()
			return
		}
		ctx := *t.ctx
		ctx.steps, ctx.locals = 0, map[string]string{}
		// like mIRC, the command was evaluated when the timer was set and is evaluated
		// again now (write $!nick to delay an identifier until the timer fires)
		// not split on | again: that would let text from the event inject commands
		ctx.command(t.cmd)
		n++
		done := t.reps > 0 && n >= t.reps
		if done {
			delete(e.timers, t.name)
		}
		e.mu.Unlock()
		if done {
			return
		}
	}
}

func (e *mircEngine) stopTimers() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopTimersLocked()
}

func (e *mircEngine) stopTimersLocked() {
	for k, t := range e.timers {
		close(t.stop)
		delete(e.timers, k)
	}
}

// ---------- variables file ----------

func (e *mircEngine) varsPath(dir string) string { return filepath.Join(dir, "data", "mirc-vars.json") }

func (e *mircEngine) loadVars(dir string) {
	if e.varsLoaded {
		return // already in memory; the file only matters at startup
	}
	e.varsLoaded = true
	e.vars = map[string]string{}
	if b, err := os.ReadFile(e.varsPath(dir)); err == nil {
		json.Unmarshal(b, &e.vars)
	}
}

// saveVarsSoon writes %variables a second after the last change (called with e.mu held).
func (e *mircEngine) saveVarsSoon() {
	if e.p == nil {
		return
	}
	if e.saveT != nil {
		e.saveT.Stop()
	}
	e.saveT = time.AfterFunc(time.Second, func() {
		e.mu.Lock()
		b, _ := json.MarshalIndent(e.vars, "", "  ")
		e.mu.Unlock()
		p := e.varsPath(e.p.pluginDir())
		os.MkdirAll(filepath.Dir(p), 0o755)
		if os.WriteFile(p+".tmp", b, 0o600) == nil {
			os.Rename(p+".tmp", p)
		}
	})
}

// ---------- events ----------

// dispatch queues a plugin event for the mIRC handlers (see Plugins.Emit). It never
// blocks: scripts run one at a time on the engine's worker.
func (e *mircEngine) dispatch(event string, c *Conn, target string, d map[string]any) {
	select {
	case e.jobs <- func() { e.handleEvent(event, c, target, d) }:
	default: // queue full: a script is stuck or flooding; drop rather than stall IRC
	}
}

func (e *mircEngine) worker() {
	for fn := range e.jobs {
		e.mu.Lock()
		fn()
		e.mu.Unlock()
	}
}

func (e *mircEngine) handleEvent(event string, c *Conn, target string, d map[string]any) {
	str := func(k string) string { s, _ := d[k].(string); return s }
	self, _ := d["self"].(bool)
	var kind string
	switch event {
	case "message":
		kind = "TEXT"
	case "action":
		kind = "ACTION"
	case "notice":
		kind = "NOTICE"
	case "raw":
		kind = "RAW"
	case "mode":
		kind = "RAWMODE"
	default:
		kind = strings.ToUpper(event)
	}
	for _, ev := range e.events {
		if ev.kind != kind {
			continue
		}
		// mIRC: on TEXT/ACTION/NOTICE are only for other people's messages
		if (kind == "TEXT" || kind == "ACTION" || kind == "NOTICE") && self {
			continue
		}
		if ev.me == 1 && !self || ev.me == -1 && self {
			continue
		}
		x := e.newCtx(c)
		x.nick = str("nick")
		if c != nil && x.nick != "" {
			x.address = c.userHost(x.nick)
		}
		ch := str("channel")
		isChan := c != nil && ch != "" && c.isChannel(ch)
		if isChan {
			x.chan_ = ch
		}
		x.target = target
		var text string
		switch kind {
		case "TEXT", "ACTION", "NOTICE":
			text = str("text")
		case "PART", "QUIT", "DISCONNECT":
			text = str("reason")
		case "KICK":
			text = str("reason")
			x.extra["knick"] = str("victim")
		case "NICK":
			x.nick = str("oldNick")
			x.extra["newnick"] = str("newNick")
		case "TOPIC":
			text = str("topic")
		case "RAWMODE":
			text = str("modes")
		case "INVITE":
			x.chan_ = ch
		case "RAW":
			ps, _ := d["params"].([]string)
			text = strings.Join(ps, " ")
			if ev.rawCode != "*" && !strings.EqualFold(ev.rawCode, str("command")) {
				continue
			}
		}
		x.params = strings.Fields(text)
		if !matchTarget(ev.target, kind, ch, isChan) {
			continue
		}
		if ev.needOp && (!isChan || !x.compare(c.Nick(), "isop", ch)) {
			continue
		}
		if ev.match != "" && kind != "RAW" || kind == "RAW" && ev.match != "*" && ev.match != "" {
			if ev.regex != nil {
				if !ev.regex.MatchString(text) {
					continue
				}
			} else if !wildMatch(strings.ToLower(text), strings.ToLower(x.eval(ev.match))) {
				continue
			}
		}
		if err := x.run(ev.body); err != nil && err != errHalt && err != errReturn {
			e.p.app.logScriptError(c, ev.file, err)
		}
	}
}

func matchTarget(t, kind, ch string, isChan bool) bool {
	switch kind {
	case "QUIT", "NICK", "CONNECT", "DISCONNECT", "START", "LOAD", "RAW":
		return true
	}
	t = strings.TrimSpace(t)
	switch t {
	case "", "*":
		return true
	case "#":
		return isChan
	case "?":
		return !isChan
	}
	for _, x := range strings.Split(t, ",") {
		if strings.EqualFold(strings.TrimSpace(x), ch) {
			return true
		}
	}
	return false
}

// onInput runs on INPUT handlers for something you typed. Returns true if a script used
// /halt or /haltdef, meaning it shouldn't be sent.
func (e *mircEngine) onInput(c *Conn, buffer, line string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	stop := false
	for _, ev := range e.events {
		if ev.kind != "INPUT" {
			continue
		}
		isChan := c.isChannel(buffer)
		if !matchTarget(ev.target, "INPUT", buffer, isChan) {
			continue
		}
		x := e.newCtx(c)
		x.nick = c.Nick()
		x.target = buffer
		if isChan {
			x.chan_ = buffer
		}
		x.params = strings.Fields(line)
		err := x.run(ev.body)
		if err != nil && err != errHalt && err != errReturn {
			e.p.app.logScriptError(c, ev.file, err)
		}
		if x.halted || x.haltdef {
			stop = true
		}
	}
	return stop
}

// runAlias runs /name args if a script defines that alias.
func (e *mircEngine) runAlias(c *Conn, buffer, name, args string) bool {
	e.mu.Lock()
	al := e.aliases[strings.ToLower(name)]
	e.mu.Unlock()
	if al == nil {
		return false
	}
	select {
	case e.jobs <- func() { e.callAlias(c, buffer, al, args) }:
	default:
	}
	return true
}

func (e *mircEngine) callAlias(c *Conn, buffer string, al *mircAlias, args string) {
	x := e.newCtx(c)
	x.target = buffer
	if c.isChannel(buffer) {
		x.chan_ = buffer
	}
	x.nick = c.Nick()
	x.params = strings.Fields(args)
	if err := x.run(al.body); err != nil && err != errHalt && err != errReturn {
		e.p.app.logScriptError(c, al.file, err)
	}
}

func (e *mircEngine) aliasNames() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for n := range e.aliases {
		out = append(out, n)
	}
	return out
}

func (e *mircEngine) hasInput() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.events {
		if ev.kind == "INPUT" {
			return true
		}
	}
	return false
}

// runStart fires on LOAD and START once after loading.
func (e *mircEngine) runStart(c *Conn) {
	e.dispatch("load", c, "", map[string]any{})
	e.dispatch("start", c, "", map[string]any{})
}

func (a *App) logScriptError(c *Conn, file string, err error) {
	if c == nil {
		c = a.plugins.firstConnected()
	}
	if c != nil {
		c.info("", "plugin", "["+file+"] "+err.Error())
	}
}

const exampleMirc = `; IRChatterbox mIRC script example. Files starting with _ aren't loaded: copy this to
; something like myscript.mrc, edit it, and type /reload.
; What's supported: docs/MIRC-SCRIPTS.md

; /hello nick  → says hello in the current channel
alias hello {
  if ($1 == $null) { echo -a Usage: /hello nick | return }
  msg $chan Hello $1 $+ , from $me!
}

; someone types !dice → roll a die
on *:TEXT:!dice:#:{
  msg $chan $nick rolled a $rand(1,6)
}

; greet people joining (not yourself)
on !*:JOIN:#:{
  notice $nick Welcome to $chan $+ , $nick $+ !
}

; count how many times each person has said hi (kept between restarts)
on *:TEXT:hi*:#:{
  inc %hi. [ $+ [ $nick ] ]
  if (%hi. [ $+ [ $nick ] ] == 1) msg $chan Welcome, $nick $+ !
}
`

func writeExampleMirc(dir string) {
	p := filepath.Join(dir, "_example.mrc")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		os.WriteFile(p, []byte(exampleMirc), 0o644)
	}
}

// clip shortens the first line of s for messages.
func clip(s string, n int) string {
	s, _, _ = strings.Cut(s, "\n")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
