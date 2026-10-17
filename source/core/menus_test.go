package core

import (
	"strings"
	"testing"
)

func TestMircMenus(t *testing.T) {
	e := mircEngineFor(t, `
menu nicklist {
  &Whois:whois $$1
  -
  Fun
  .Slap:me slaps $$1 around a bit with a large trout
  .Hug:{
    me hugs $$1
    inc %hugs
  }
  $iif($1 == bob, Bob only):echo bob
  Op tools
  ..Too deep:echo x
}
menu channel,query {
  Say hi:say Hi $chan
}
menu @custom { X:echo x }
menu menubar { Y:echo y }
`)
	if len(e.menus) != 2 {
		t.Fatalf("menus %d", len(e.menus))
	}
	labels := func(items []MenuItem) string {
		var b []string
		for _, it := range items {
			switch {
			case it.Sep:
				b = append(b, "-")
			case it.Items != nil:
				var sub []string
				for _, s := range it.Items {
					sub = append(sub, s.Label)
				}
				b = append(b, it.Label+"["+strings.Join(sub, ",")+"]")
			default:
				b = append(b, it.Label)
			}
		}
		return strings.Join(b, "|")
	}
	got := labels(e.menuItems(nil, MenuContext{Kind: "nicklist", Nick: "bob"}))
	if got != "Whois|-|Fun[Slap,Hug]|Bob only|Op tools[Too deep]" {
		t.Errorf("nicklist for bob: %s", got)
	}
	got = labels(e.menuItems(nil, MenuContext{Kind: "nicklist", Nick: "amy"}))
	if got != "Whois|-|Fun[Slap,Hug]|Op tools[Too deep]" {
		t.Errorf("nicklist for amy: %s", got)
	}
	if got = labels(e.menuItems(nil, MenuContext{Kind: "query", Buffer: "amy"})); got != "Say hi" {
		t.Errorf("query: %s", got)
	}
	if got = labels(e.menuItems(nil, MenuContext{Kind: "status"})); got != "" {
		t.Errorf("status: %s", got)
	}
	// the multi-line Hug block parsed into two statements
	for _, n := range e.menuNodes {
		if n.label == "Hug" && len(n.body) != 2 {
			t.Errorf("Hug body %+v", n.body)
		}
	}
	// $$1 stops the command when there's no nick
	x := e.newCtx(nil)
	if err := x.command("echo $$1"); err != errHalt {
		t.Errorf("$$1 without a value: %v", err)
	}
	x.params = []string{"bob"}
	if err := x.command("echo $$1"); err != nil {
		t.Errorf("$$1 with a value: %v", err)
	}
}

func TestTidySeps(t *testing.T) {
	in := []MenuItem{{Sep: true}, {Label: "a"}, {Sep: true}, {Sep: true}, {Label: "b"}, {Sep: true}}
	out := tidySeps(in)
	if len(out) != 3 || out[1].Sep != true {
		t.Fatalf("%+v", out)
	}
}
