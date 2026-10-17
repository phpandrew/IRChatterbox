//go:build integration

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Script menu items, from both kinds of script, against ergo on 127.0.0.1:6667.
func TestPluginMenus(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "menu")
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("HOME", tmp)
	id := newID()[:4]
	ch := "#menu" + id
	a := NewApp()
	log := &evLog{}
	a.testHook = log.add
	a.store.Update(func(c *Config) { c.Servers = nil; c.Plugins.ScriptsEnabled = true })
	dir := a.plugins.pluginDir()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "m.mrc"), []byte("menu nicklist {\n  Slap:me slaps $$1 with a trout\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "m.js"), []byte(`
menu("nicklist,query", "Fun/Wave", function (e) { say(e.channel, "waves at " + e.nick); });
menu("message", "Quote", function (e) { reply("> " + e.text); });
`), 0o644)
	if msg := a.plugins.LoadScripts(); !strings.Contains(msg, "m.js") || !strings.Contains(msg, "m.mrc") || strings.Contains(msg, "error") {
		t.Fatal(msg)
	}
	sv := a.SaveServer(ServerConfig{Name: "Ergo", Host: "127.0.0.1", Port: 6667, Nick: "mn" + id, AutoJoin: []string{ch}})
	a.Connect(sv.ID)
	log.waitFor(t, "joined", func(e Event) bool { return e.Type == "buffer" && e.Buffer == ch })
	bob := dialRaw(t, "bob"+id)
	bob.send("JOIN " + ch)
	bob.waitFor(t, "366", 3*time.Second)

	mc := MenuContext{Server: sv.ID, Buffer: ch, Kind: "nicklist", Nick: "bob" + id}
	items := a.PluginMenu(mc)
	if len(items) != 2 || items[0].Label != "Slap" || items[1].Label != "Fun" || len(items[1].Items) != 1 {
		t.Fatalf("items %+v", items)
	}
	a.RunPluginMenu(mc, items[0].ID)
	bob.waitFor(t, "ACTION slaps bob"+id+" with a trout", 4*time.Second)
	a.RunPluginMenu(mc, items[1].Items[0].ID)
	bob.waitFor(t, "waves at bob"+id, 4*time.Second)

	msg := MenuContext{Server: sv.ID, Buffer: ch, Kind: "message", Nick: "bob" + id, Text: "nice one"}
	items = a.PluginMenu(msg)
	if len(items) != 1 {
		t.Fatalf("message items %+v", items)
	}
	a.RunPluginMenu(msg, items[0].ID)
	bob.waitFor(t, "> nice one", 4*time.Second)

	if got := a.PluginMenu(MenuContext{Server: sv.ID, Kind: "status"}); len(got) != 0 {
		t.Fatalf("status %+v", got)
	}
}
