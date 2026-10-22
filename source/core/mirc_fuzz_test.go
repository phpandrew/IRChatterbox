package core

import (
	"fmt"
	"runtime/debug"
	"strings"
	"testing"
)

// Script built-ins get text from the channel as arguments: odd values must not crash.
func TestMircIdentifiersWithHostileArgs(t *testing.T) {
	e := mircEngineFor(t, "")
	idents := []string{"len", "upper", "lower", "left", "right", "mid", "replace", "replacecs", "remove", "pos", "count", "str", "strip",
		"chr", "asc", "calc", "int", "abs", "round", "iif", "gettok", "numtok", "istok", "findtok", "addtok", "deltok", "remtok", "hget",
		"duration", "asctime", "rand", "time", "date", "ctime", "var", "comchan", "nick", "address", "mask", "upper", "rand"}
	vals := []string{"", "0", "-1", "-99999999999", "99999999999", "1e308", "NaN", "a", "$", "%", "(", ")", ",", "32", "-0",
		"9223372036854775807", "-9223372036854775808", "\x00", "ä😀", strings.Repeat("x", 300), "1.5", "1-", "-3-", "0-0", "*", "2^9999", "1/0", "((((("}
	n := 0
	for _, id := range idents {
		for i, a := range vals {
			for j := 0; j < len(vals); j += 3 {
				b, c := vals[j], vals[(i+j)%len(vals)]
				for _, call := range []string{
					fmt.Sprintf("$%s(%s)", id, a), fmt.Sprintf("$%s(%s,%s)", id, a, b), fmt.Sprintf("$%s(%s,%s,%s)", id, a, b, c),
					fmt.Sprintf("$%s(%s,%s,%s,%s)", id, a, b, c, a), fmt.Sprintf("$%s($1-,$2,$3)", id),
				} {
					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Fatalf("%s with $1-=%q $2=%q $3=%q: %v\n%s", call, a, b, c, r, debug.Stack())
							}
						}()
						mircEval(e, call, a, b, c)
						x := e.newCtx(nil)
						x.params = []string{a, b, c}
						x.cond(fmt.Sprintf("(%s isin %s) && ($calc(%s) > %s)", a, b, c, a))
					}()
					n++
				}
			}
		}
	}
	t.Logf("%d calls", n)
}
