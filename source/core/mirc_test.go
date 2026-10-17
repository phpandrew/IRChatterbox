package core

import (
	"strings"
	"testing"
	"time"
)

// mircRun loads src and calls $test or /test with no connection, returning what /echo printed
// (captured via %out) or the alias's /return value.
func mircEngineFor(t *testing.T, src string) *mircEngine {
	t.Helper()
	e := &mircEngine{aliases: map[string]*mircAlias{}, vars: map[string]string{}, hash: map[string]map[string]string{}, timers: map[string]*mircTimer{}, jobs: make(chan func(), 10)}
	for _, w := range e.parseFile("t.mrc", src) {
		t.Logf("warning: %s", w)
	}
	return e
}

func mircEval(e *mircEngine, expr string, params ...string) string {
	x := e.newCtx(nil)
	x.params = params
	return x.eval(expr)
}

func TestMircIdentifiers(t *testing.T) {
	e := mircEngineFor(t, "")
	cases := []struct {
		expr   string
		params []string
		want   string
	}{
		{"$1", []string{"a", "b", "c"}, "a"},
		{"$2-", []string{"a", "b", "c"}, "b c"},
		{"$1-2", []string{"a", "b", "c"}, "a b"},
		{"$0", []string{"a", "b", "c"}, "3"},
		{"$upper($1) $+ !", []string{"hi"}, "HI!"},
		{"$len($1-)", []string{"ab", "cd"}, "5"},
		{"$left(hello,2) $right(hello,3) $mid(hello,2,3)", nil, "he llo ell"},
		{"$gettok(a.b.c,2,46)", nil, "b"},
		{"$gettok(a.b.c,2-,46)", nil, "b.c"},
		{"$gettok(a.b.c,-1,46)", nil, "c"},
		{"$numtok(a b c d,32)", nil, "4"},
		{"$addtok(a b,c,32)", nil, "a b c"},
		{"$deltok(a b c,2,32)", nil, "a c"},
		{"$istok(a b c,B,32)", nil, "$true"},
		{"$calc(2 + 3 * 4)", nil, "14"},
		{"$calc((2+3)*4 / 8)", nil, "2.5"},
		{"$calc(2^10 % 1000)", nil, "24"},
		{"$replace(Hello World,world,there)", nil, "Hello there"},
		{"$remove(a-b-c,-)", nil, "abc"},
		{"$pos(hello,l)", nil, "3"},
		{"$count(banana,a)", nil, "3"},
		{"$str(ab,3)", nil, "ababab"},
		{"$iif($1 == x,yes,no)", []string{"x"}, "yes"},
		{"$iif($1 == x,yes,no)", []string{"y"}, "no"},
		{"$duration(3725)", nil, "1hr 2mins 5secs"},
		{"$round(3.14159,2)", nil, "3.14"},
		{"$chr(65) $asc(a)", nil, "A 97"},
		{"$lower($upper(MiXeD))", nil, "mixed"},
		{"100%", nil, "100%"},
	}
	for _, c := range cases {
		if got := mircEval(e, c.expr, c.params...); got != c.want {
			t.Errorf("%s with %v = %q, want %q", c.expr, c.params, got, c.want)
		}
	}
}

func TestMircConditions(t *testing.T) {
	e := mircEngineFor(t, "")
	x := e.newCtx(nil)
	x.params = []string{"!hello", "world"}
	yes := []string{"$1 == !HELLO", "$1 === !hello", "5 > 3", "10 >= 10", "abc < abd", "ell isin $1",
		"!hel* iswm $1", "5 isnum", "5 isnum 1-10", "4 // 2", "5 \\\\ 2", "$2 isalpha", "1 == 1 && 2 == 2",
		"1 == 2 || 2 == 2", "!(1 == 2)", "$1", "!$3", "$len($1-) > 5", "$iif(1 == 1, a, b) == a"}
	no := []string{"$1 === !HELLO", "3 > 5", "abc isin $1", "x isnum", "11 isnum 1-10", "1 == 1 && 2 == 3", "$3", "0", "$false"}
	for _, c := range yes {
		if !x.cond(c) {
			t.Errorf("expected true: %s", c)
		}
	}
	for _, c := range no {
		if x.cond(c) {
			t.Errorf("expected false: %s", c)
		}
	}
}

func TestMircControlFlow(t *testing.T) {
	e := mircEngineFor(t, `
; comment
/* block
comment */
alias fact {
  var %n = $1, %r = 1
  while (%n > 1) {
    %r = %r * %n
    dec %n
  }
  return %r
}
alias grade {
  if ($1 >= 90) { return A }
  elseif ($1 >= 80) return B
  else { return C }
}
alias nested {
  if ($1 == a) {
    if ($2 == b) { return ab }
    else return a-only
  }
  else if ($1 == c) { return c }
  return none
}
alias loop {
  var %i = 0, %s
  while (%i < 10) {
    inc %i
    if (%i == 3) continue
    if (%i == 6) break
    %s = %s $+ %i
  }
  return %s
}
alias oneliner return $1 $+ - $+ $2 | echo unused
alias counter { inc %hits | return %hits }
alias tok return $gettok($1-,2,32)
alias greet return Hello, $1 $+ !
alias usesgreet return $greet($upper($1))
alias forever { while (1) { inc %spin } }
`)
	cases := map[string]string{
		"$fact(5)":          "120",
		"$grade(95)":        "A",
		"$grade(85)":        "B",
		"$grade(10)":        "C",
		"$nested(a,b)":      "ab",
		"$nested(a,x)":      "a-only",
		"$nested(c)":        "c",
		"$nested(z)":        "none",
		"$loop":             "1245",
		"$oneliner(x,y)":    "x-y",
		"$tok(one two)":     "two",
		"$usesgreet(bob)":   "Hello, BOB!",
		"$counter $counter": "1 2",
	}
	for expr, want := range cases {
		if got := mircEval(e, expr); got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}
	// infinite loops are stopped
	x := e.newCtx(nil)
	if err := x.run(e.aliases["forever"].body); err != errBudget {
		t.Errorf("runaway loop: got %v, want the step limit", err)
	}
	// globals set with /set or %x = persist across calls; var is local
	if e.vars["%hits"] != "2" {
		t.Errorf("%%hits = %q", e.vars["%hits"])
	}
	if _, leaked := e.vars["%n"]; leaked {
		t.Error("local var leaked into globals")
	}
}

func TestMircParseEvents(t *testing.T) {
	e := mircEngineFor(t, `
on *:TEXT:!hi*:#:{ msg $chan Hi $nick }
on $*:TEXT:/^!w(eather)?\s/i:*:{ return }
on me:*:JOIN:#test:echo joined
on !*:JOIN:#:msg $chan welcome $nick
on *:NOTICE:*:?:halt
on *:INPUT:#:{ if ($1 == hi) halt }
raw 332:*:echo topic $3-
on *:DIALOG:x:init:0:{ }
menu channel { Hi:msg # hi }
dialog test { title "x" }
`)
	if len(e.events) != 7 {
		for _, ev := range e.events {
			t.Logf("%s %q %q", ev.kind, ev.match, ev.target)
		}
		t.Fatalf("got %d events, want 7", len(e.events))
	}
	ev := e.events[1]
	if ev.regex == nil || !ev.regex.MatchString("!W london") || ev.regex.MatchString("!wx") {
		t.Error("regex event")
	}
	if e.events[2].me != 1 || e.events[3].me != -1 || e.events[2].target != "#test" {
		t.Error("me:/! levels")
	}
	if e.events[6].kind != "RAW" || e.events[6].rawCode != "332" {
		t.Error("raw event")
	}
	if !wildMatch("!hi there", "!hi*") || wildMatch("x!hi", "!hi*") || !wildMatch("a b", "& &") {
		t.Error("wildcards")
	}
}

func TestMircTimerArgs(t *testing.T) {
	e := mircEngineFor(t, "")
	x := e.newCtx(nil)
	x.nick = "bob"
	// $!nick survives the first evaluation, to be evaluated when the timer fires
	if got := x.eval("msg #c $nick $!nick"); got != "msg #c bob $nick" {
		t.Errorf("got %q", got)
	}
	if got := strings.Join(strings.Fields(mircTimeFormat(mustTime("2026-03-04T05:06:07Z"), "yyyy-mm-dd HH:nn:ss ddd")), " "); got != "2026-03-04 05:06:07 Wed" {
		t.Errorf("time format %q", got)
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestMircBrackets(t *testing.T) {
	e := mircEngineFor(t, `alias b { set %count. [ $+ [ $1 ] ] 5 | inc %count. [ $+ [ $1 ] ] | return %count. [ $+ [ $1 ] ] }`)
	if got := mircEval(e, "$b(bob)"); got != "6" {
		t.Errorf("got %q", got)
	}
	if e.vars["%count.bob"] != "6" {
		t.Errorf("vars %v", e.vars)
	}
}

func TestMircJoinIdent(t *testing.T) {
	e := mircEngineFor(t, "")
	if got := mircEval(e, "$+(a,$upper(b),c) x $mask(bob!~u@h.com,2)", "q"); got != "aBc x *!*@h.com" {
		t.Errorf("got %q", got)
	}
}

func TestMircExample(t *testing.T) {
	e := &mircEngine{aliases: map[string]*mircAlias{}, vars: map[string]string{}, hash: map[string]map[string]string{}, timers: map[string]*mircTimer{}}
	if w := e.parseFile("_example.mrc", exampleMirc); len(w) > 0 || len(e.events) != 3 || e.aliases["hello"] == nil {
		t.Fatalf("example: warnings %v, %d events", w, len(e.events))
	}
}

func TestMircCharCodes(t *testing.T) {
	e := mircEngineFor(t, "")
	for expr, want := range map[string]string{
		"$chr(65)":                    "A",
		"$chr(-1)":                    "",
		"$chr(99999999999)":           "",
		"$chr(abc)":                   "",
		"$gettok(a.b.c,2,46)":         "b",
		"$gettok(a b c,2,4294967328)": "b", // overflow falls back to a space
		"$numtok(a b,-5)":             "2",
	} {
		if got := mircEval(e, expr); got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}
}
