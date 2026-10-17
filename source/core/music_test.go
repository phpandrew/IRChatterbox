//go:build integration

package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMusicTrigger(t *testing.T) {
	tmp := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmp)
	f := filepath.Join(tmp, "np.txt")
	os.WriteFile(f, []byte("Daft Punk\nOne More Time\n"), 0o644)
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	a.store.Update(func(c *Config) {
		c.Plugins.Music.Source = "file"
		c.Plugins.Music.File = f
		c.Plugins.Music.Cooldown = 0
	})
	sv := a.SaveServer(ServerConfig{Name: "L", Host: "127.0.0.1", Port: 6667, Nick: "Mu" + newID()[:4], AutoJoin: []string{"#music"}})
	a.Connect(sv.ID)
	log.waitFor(t, "join", func(e Event) bool { return e.Type == "buffer" && e.Buffer == "#music" })
	bob := dialRaw(t, "bob"+newID()[:3])
	bob.send("JOIN #music")
	bob.waitFor(t, "366", 3*time.Second)
	bob.send("PRIVMSG #music :!music")
	t.Log(bob.waitFor(t, "[Bot] Now playing: Daft Punk - One More Time", 4*time.Second))
	a.SendInput(sv.ID, "#music", "/np")
	t.Log(bob.waitFor(t, "ACTION is listening to Daft Punk - One More Time", 4*time.Second))
}
