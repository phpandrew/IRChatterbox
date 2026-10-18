package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdata/soju-history.txt is CHATHISTORY output from the IRChatterbox soju build after the
// account site set this throwaway public key. The first two lines were stored before the key
// existed and encrypted afterwards; the third was encrypted as it arrived.
const testEncKey = "AGE-SECRET-KEY-1PRR7AG65MMLURQ0AD5XM8LTNJNNUZ4DWWJ5MNUQ5RA8U49M7D3CSJQKN8Y"

func encTestConn(t *testing.T, key string) *Conn {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)
	a := NewApp()
	a.store.Update(func(c *Config) { c.Servers = nil })
	sv := a.SaveServer(ServerConfig{Name: "Bouncer", Host: "127.0.0.1", Port: 6697, Nick: "sam", EncryptionKey: key})
	return &Conn{app: a, cfg: sv}
}

func sojuHistory(t *testing.T) []*Message {
	b, err := os.ReadFile(filepath.Join("testdata", "soju-history.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []*Message
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		out = append(out, ParseMessage(strings.TrimRight(l, "\r")))
	}
	if len(out) != 3 {
		t.Fatalf("want 3 history lines, got %d", len(out))
	}
	return out
}

func TestDecryptSojuHistory(t *testing.T) {
	want := []string{"hello one, before the key", "\x01ACTION waves before the key\x01", "secret two, after the key"}

	// a pasted age-keygen file, with its comments, works as it is
	c := encTestConn(t, "# created: 2026-10-18\n# public key: age1tpv2...\n"+testEncKey+"\n")
	for i, m := range sojuHistory(t) {
		if m.Params[1] == want[i] {
			t.Fatal("history line was not encrypted")
		}
		c.decryptStored(m)
		if m.Params[1] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, m.Params[1], want[i])
		}
		if _, ok := m.Tags[encTag]; ok {
			t.Error("ciphertext tag should be removed once read")
		}
	}

	// an old key plus a new one: either can open its messages
	other, _ := (&App{}).GenerateEncryptionKey()
	c = encTestConn(t, other.PrivateKey+"\n"+testEncKey)
	m := sojuHistory(t)[2]
	c.decryptStored(m)
	if m.Params[1] != want[2] {
		t.Errorf("with two keys: got %q", m.Params[1])
	}

	for name, tc := range map[string]struct{ key, text string }{
		"no key":    {"", encNoKeyText},
		"wrong key": {other.PrivateKey, encWrongKeyText},
	} {
		c := encTestConn(t, tc.key)
		m := sojuHistory(t)[0]
		c.decryptStored(m)
		if m.Params[1] != tc.text {
			t.Errorf("%s: got %q", name, m.Params[1])
		}
	}

	c = encTestConn(t, testEncKey)
	m = sojuHistory(t)[1]
	m.Tags[encTag] = m.Tags[encTag][:40] + "AAAA" + m.Tags[encTag][44:]
	c.decryptStored(m)
	if m.Params[1] != encDamagedText {
		t.Errorf("damaged: got %q", m.Params[1])
	}

	// ordinary lines are left alone
	m = ParseMessage(":bob!b@h PRIVMSG #test :plain words")
	c.decryptStored(m)
	if m.Params[1] != "plain words" {
		t.Errorf("plain line changed: %q", m.Params[1])
	}
}

func TestEncryptionKeyHelpers(t *testing.T) {
	a := &App{}
	kp, err := a.GenerateEncryptionKey()
	if err != nil || !strings.HasPrefix(kp.PrivateKey, "AGE-SECRET-KEY-1") || !strings.HasPrefix(kp.PublicKey, "age1") {
		t.Fatalf("generate: %+v %v", kp, err)
	}
	pubs, err := a.EncryptionPublicKeys(kp.PrivateKey + "\n" + testEncKey)
	if err != nil || len(pubs) != 2 || pubs[0] != kp.PublicKey || pubs[1] != "age1tpv2mane7npmdf8rgu66jeh7r5yv7tu3h4fh040vyn3wq8ssta0sytk4wd" {
		t.Fatalf("public keys: %v %v", pubs, err)
	}
	if _, err := a.EncryptionPublicKeys(kp.PublicKey); err == nil {
		t.Error("a public key pasted as the private key should be refused")
	}
}
