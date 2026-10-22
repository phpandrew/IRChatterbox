package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretsInCredentialStore(t *testing.T) {
	mem := &memSecrets{m: map[string]string{}}
	useSecretBackend(mem)
	defer useSecretBackend(nil)
	dir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", dir)
	os.Setenv("APPDATA", dir)

	a := NewApp()
	sv := a.SaveServer(ServerConfig{Name: "Libera", Host: "irc.libera.chat", Port: 6697, Nick: "me", AuthMethod: "sasl", AuthPass: "s4sl-secret", Password: "srvpass", ProxyPass: "pxpass"})
	raw, _ := os.ReadFile(filepath.Join(a.store.dir, "config.json"))
	for _, secret := range []string{"s4sl-secret", "srvpass", "pxpass"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("%s written to config.json", secret)
		}
	}
	if !strings.Contains(string(raw), `"secretsStored": true`) {
		t.Fatal("not marked as stored")
	}
	if mem.m[sv.ID+"/authPass"] != "s4sl-secret" {
		t.Fatalf("store: %v", mem.m)
	}
	// the running app still has them
	if got, _ := a.store.Server(sv.ID); got.AuthPass != "s4sl-secret" || got.Password != "srvpass" {
		t.Fatalf("%+v", got)
	}

	// a fresh start reads them back
	useSecretBackend(mem)
	b := NewApp()
	if got, _ := b.store.Server(sv.ID); got.AuthPass != "s4sl-secret" || got.ProxyPass != "pxpass" {
		t.Fatalf("after restart: %+v", got)
	}
	// clearing a password removes it from the store; deleting the server removes the rest
	got, _ := b.store.Server(sv.ID)
	got.Password = ""
	b.SaveServer(got)
	if _, ok := mem.m[sv.ID+"/password"]; ok {
		t.Fatal("cleared password still stored")
	}
	b.DeleteServer(sv.ID)
	for k := range mem.m {
		if strings.HasPrefix(k, sv.ID+"/") {
			t.Fatalf("left behind: %s", k)
		}
	}
}

func TestSecretsFallBackWithoutStore(t *testing.T) {
	useSecretBackend(nil)
	defer useSecretBackend(nil)
	a := newBackupApp(t)
	sv := a.SaveServer(ServerConfig{Name: "X", Host: "h", Port: 6667, Nick: "me", AuthPass: "plain-ok"})
	raw, _ := os.ReadFile(filepath.Join(a.store.dir, "config.json"))
	if !strings.Contains(string(raw), "plain-ok") || strings.Contains(string(raw), "secretsStored") {
		t.Fatal("without a credential store the password should stay in config.json")
	}
	if got, _ := a.store.Server(sv.ID); got.AuthPass != "plain-ok" {
		t.Fatal(got.AuthPass)
	}
}

// Moving an existing plain-text config into the store happens on the first save.
func TestSecretsMigrateExistingConfig(t *testing.T) {
	useSecretBackend(nil)
	a := newBackupApp(t)
	sv := a.SaveServer(ServerConfig{Name: "Old", Host: "h", Port: 6667, Nick: "me", BouncerPass: "bnc-old"})
	mem := &memSecrets{m: map[string]string{}}
	useSecretBackend(mem)
	defer useSecretBackend(nil)
	b := NewApp() // reads the plain-text file
	b.store.Update(func(c *Config) {})
	raw, _ := os.ReadFile(filepath.Join(b.store.dir, "config.json"))
	if strings.Contains(string(raw), "bnc-old") || mem.m[sv.ID+"/bouncerPass"] != "bnc-old" {
		t.Fatal("not migrated")
	}
}

// A credential store that can't be opened this time doesn't lose the link to the passwords.
func TestSecretsStoreUnavailableKeepsLink(t *testing.T) {
	mem := &memSecrets{m: map[string]string{}}
	useSecretBackend(mem)
	a := newBackupApp(t)
	sv := a.SaveServer(ServerConfig{Name: "L", Host: "h", Port: 6667, Nick: "me", AuthPass: "keep-me"})
	useSecretBackend(nil) // locked / missing this launch
	b := NewApp()
	b.store.Update(func(c *Config) {})
	raw, _ := os.ReadFile(filepath.Join(b.store.dir, "config.json"))
	if !strings.Contains(string(raw), `"secretsStored": true`) {
		t.Fatal("link to the stored passwords lost")
	}
	useSecretBackend(mem) // back next time
	defer useSecretBackend(nil)
	c := NewApp()
	if got, _ := c.store.Server(sv.ID); got.AuthPass != "keep-me" {
		t.Fatalf("password not back: %q", got.AuthPass)
	}
}

func TestClientCertChecks(t *testing.T) {
	a := newBackupApp(t)
	c1, c2 := a.GenerateClientCert("one"), a.GenerateClientCert("two")
	if c1.Error != "" || c1.Subject != "one" || c1.Fingerprint == c2.Fingerprint {
		t.Fatalf("%+v", c1)
	}
	if d := a.DescribeClientCert(c1.Cert, c2.Key); d.Error == "" {
		t.Fatal("mismatched key accepted")
	}
	if _, err := (ServerConfig{ClientCert: c1.Cert}).tlsClientCerts(); err == nil {
		t.Fatal("certificate without its key used")
	}
	a.SaveServer(ServerConfig{Name: "C", Host: "h", Port: 6697, TLS: true, Nick: "me", ClientCert: c1.Cert, ClientKey: c1.Key})
	if b := a.ExportSettings(false, ""); strings.Contains(b.Data, "PRIVATE KEY") {
		t.Fatal("private key in a backup without passwords")
	}
}
