package core

import "testing"

// The same network set up on two devices before turning on sync ends up as one server on
// each, keeping each device's own login, instead of each device adding the other's copy.
func TestSyncSameNetworkOnBothDevices(t *testing.T) {
	_, api := newFakeSyncServer(t)
	a, b := newBackupApp(t), newBackupApp(t)
	cats := map[string]bool{"servers": true, "nickColors": true}

	setup := func(x *App, pass string) string {
		sv := x.SaveServer(ServerConfig{Name: "Neowin", Host: "chatter.example", Port: 6697, TLS: true, Nick: "Andrew",
			Bouncer: "soju", BouncerUser: "andrew", BouncerPass: pass, BouncerNetwork: "neowin"})
		x.store.Update(func(c *Config) { c.Favourites = append(c.Favourites, Favourite{Server: sv.ID, Buffer: "#neowin"}) })
		return sv.ID
	}
	aID := setup(a, "pass-a")
	bID := setup(b, "pass-b")

	syncSignIn(t, a, api, "andrew@example.com")
	start := a.SyncStart(cats)
	if start.Error != "" {
		t.Fatal(start.Error)
	}
	syncSignIn(t, b, api, "andrew@example.com")
	if r := b.SyncJoin(start.Code, cats); r.Error != "" {
		t.Fatal(r.Error)
	}
	mustCycle(t, a, b, a, b, a, b)

	check := func(name string, x *App, id, pass string, favs int) {
		t.Helper()
		c := x.store.Get()
		n := 0
		for _, s := range c.Servers {
			if s.Host == "chatter.example" {
				n++
				if s.ID != id || s.BouncerPass != pass {
					t.Fatalf("%s kept the wrong copy: %+v", name, s)
				}
			}
		}
		if n != 1 {
			t.Fatalf("%s has %d copies of the network", name, n)
		}
		if len(c.Favourites) != favs {
			t.Fatalf("%s favourites: %+v", name, c.Favourites)
		}
		for _, f := range c.Favourites {
			if f.Server != id {
				t.Fatalf("%s favourite on the wrong server: %+v", name, c.Favourites)
			}
		}
	}
	check("A", a, aID, "pass-a", 1)
	check("B", b, bID, "pass-b", 1)

	// a copy left by an earlier version (B added A's server as a second one) is merged away
	stB := b.loadSyncState()
	keyA := stB.Servers[bID] // the sync ID both now use
	dup := b.SaveServer(ServerConfig{Name: "Neowin", Host: "chatter.example", Port: 6697, TLS: true, Nick: "Andrew",
		Bouncer: "soju", BouncerNetwork: "neowin"})
	b.store.Update(func(c *Config) {
		c.Favourites = append(c.Favourites, Favourite{Server: dup.ID, Buffer: "#neowin"}, Favourite{Server: dup.ID, Buffer: "#lounge"})
	})
	stB.Servers[dup.ID] = "someone-elses-key"
	b.saveSyncState(stB)
	mustCycle(t, b, a, b, a)
	check2 := b.store.Get()
	n := 0
	for _, s := range check2.Servers {
		if s.Host == "chatter.example" {
			n++
		}
	}
	if n != 1 || len(check2.Favourites) != 2 {
		t.Fatalf("after merge: %d copies, favourites %+v", n, check2.Favourites)
	}
	for _, f := range check2.Favourites {
		if f.Server != bID {
			t.Fatalf("favourite not moved to the kept server: %+v", f)
		}
	}
	if b.loadSyncState().Servers[bID] != keyA {
		t.Fatal("the kept server changed its sync ID")
	}
	// #lounge, moved to B's real server, reaches A as an ordinary favourite
	check("A after B's cleanup", a, aID, "pass-a", 2)
}
