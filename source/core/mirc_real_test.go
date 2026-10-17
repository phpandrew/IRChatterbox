package core

import "testing"

// Shapes taken from commonly shared scripts.
func TestMircRealScripts(t *testing.T) {
	e := mircEngineFor(t, `
on *:START:{
  if (!$hget(seen)) hmake seen 100
}
on *:TEXT:*:#:{
  hadd seen $nick $ctime $chan $1-
  if ($1 == !seen) {
    if (!$2) { msg $chan Usage: !seen <nick> | halt }
    if ($hget(seen,$2)) {
      var %data = $hget(seen,$2)
      msg $chan $2 was last seen $duration($calc($ctime - $gettok(%data,1,32))) ago in $gettok(%data,2,32) saying: $gettok(%data,3-,32)
    }
    else { msg $chan I haven't seen $2 }
  }
}
alias -l helper { return $+($1,-,$2) }
on *:KICK:#:{
  if ($knick == $me) { join $chan | msg $chan Please don't kick me, $nick }
}
on @*:JOIN:#ops:mode $chan +v $nick
ctcp *:VERSION:*:ctcpreply $nick VERSION mine
#group on
alias grouped return yes
#group end
`)
	if len(e.events) != 4 {
		t.Fatalf("events %d", len(e.events))
	}
	if e.aliases["helper"] == nil || e.aliases["grouped"] == nil {
		t.Fatalf("aliases %v", e.aliases)
	}
	x := e.newCtx(nil)
	x.run(e.events[0].body)
	if e.hash["seen"] == nil {
		t.Fatal("hmake")
	}
	x = e.newCtx(nil)
	x.nick, x.chan_, x.params = "bob", "#c", []string{"hello", "world"}
	x.run(e.events[1].body)
	if e.hash["seen"]["bob"] == "" {
		t.Fatal("hadd")
	}
	x.params = []string{"!seen", "bob"}
	if got := x.eval("$gettok($hget(seen,$2),3-,32)"); got != "hello world" {
		t.Fatalf("got %q", got)
	}
}
