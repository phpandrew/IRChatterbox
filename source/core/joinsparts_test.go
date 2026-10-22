package core

import "testing"

func TestChannelJoinsAndIconText(t *testing.T) {
	a := newBackupApp(t)
	s := a.SaveServer(ServerConfig{Name: "TLeech", Host: "irc.example", IconText: " t l x", JoinsParts: "bogus"})
	if s.IconText != "tl" || s.JoinsParts != "" {
		t.Fatalf("saved %q %q", s.IconText, s.JoinsParts)
	}
	if got := cleanIconText("😀🎉x"); got != "😀🎉" {
		t.Fatalf("emoji %q", got)
	}
	a.SetChannelJoins(s.ID, "#TorrentLeech", "hide")
	a.SetChannelJoins(s.ID, "#x", "nope")
	cj := a.store.Get().ChannelJoins
	if cj[s.ID+"/#torrentleech"] != "hide" || len(cj) != 1 {
		t.Fatalf("set %v", cj)
	}
	a.SetChannelJoins(s.ID, "#torrentleech", "")
	if len(a.store.Get().ChannelJoins) != 0 {
		t.Fatal("not cleared")
	}
	a.SetChannelJoins(s.ID, "#a", "show")
	a.DeleteServer(s.ID)
	if len(a.store.Get().ChannelJoins) != 0 {
		t.Fatal("left behind after removing the server")
	}
}

func TestServerJoins(t *testing.T) {
	a := newBackupApp(t)
	s := a.SaveServer(ServerConfig{Name: "n", Host: "h", Password: "secret"})
	a.SetServerJoins(s.ID, "hide")
	got := a.store.Get().Servers[0]
	if got.JoinsParts != "hide" || got.Password != "secret" {
		t.Fatalf("%+v", got)
	}
	a.SetServerJoins(s.ID, "weird")
	if a.store.Get().Servers[0].JoinsParts != "" {
		t.Fatal("not reset")
	}
}
