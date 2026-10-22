package core

// Passwords live in the operating system's credential store when it has one: Windows
// Credential Manager, the macOS Keychain or the Linux Secret Service (GNOME Keyring,
// KWallet). config.json then holds the rest of the settings with those fields empty and
// "secretsStored": true. Where no store is available (some Linux setups, Android, tests)
// they stay in config.json as before, so a login never fails because of this.
//
// Kept in the store, per server: the server password, SASL/NickServ password, bouncer
// password, proxy password and the encrypted-history key.

import (
	"errors"
	"log"
	"sync"
)

const keyringService = "IRChatterbox"

// secretBackend is the credential store; tests swap in a map.
type secretBackend interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

var errSecretNotFound = errors.New("secret not found")

var (
	secretsMu       sync.Mutex
	secrets         secretBackend         = osSecretBackend()
	secretsUsable   *bool                 // probed once
	secretsWritten  = map[string]string{} // what the store already holds (saves rewriting it)
	secretsByServer = map[string]bool{}   // servers with secrets in the store
	secretsUnread   = map[string]bool{}   // servers whose stored secrets couldn't be read this time
)

// secretFields are the ServerConfig fields kept in the store.
func secretFields(sv *ServerConfig) map[string]*string {
	return map[string]*string{
		"password": &sv.Password, "authPass": &sv.AuthPass, "bouncerPass": &sv.BouncerPass,
		"proxyPass": &sv.ProxyPass, "encryptionKey": &sv.EncryptionKey, "clientKey": &sv.ClientKey,
	}
}

func secretKey(serverID, field string) string { return serverID + "/" + field }

// secretStoreUsable checks once whether the credential store works (it may be missing,
// locked or refuse us). secretsMu must be held.
func secretStoreUsable() bool {
	if secretsUsable != nil {
		return *secretsUsable
	}
	ok := secrets != nil
	if ok {
		const probe = "_probe"
		ok = secrets.Set(probe, "ok") == nil
		if ok {
			v, err := secrets.Get(probe)
			ok = err == nil && v == "ok"
			secrets.Delete(probe)
		}
	}
	if !ok {
		log.Printf("credential store not available: passwords stay in config.json")
	}
	secretsUsable = &ok
	return ok
}

// loadSecrets fills in the passwords of servers whose secrets are in the store.
func loadSecrets(c *Config) {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	for i := range c.Servers {
		sv := &c.Servers[i]
		if !sv.SecretsStored {
			continue
		}
		if !secretStoreUsable() {
			log.Printf("%s: passwords are in the credential store, which can't be opened", sv.Name)
			secretsUnread[sv.ID] = true
			continue
		}
		secretsByServer[sv.ID] = true
		for field, p := range secretFields(sv) {
			k := secretKey(sv.ID, field)
			v, err := secrets.Get(k)
			if err != nil {
				if !errors.Is(err, errSecretNotFound) {
					log.Printf("%s: reading %s from the credential store: %v", sv.Name, field, err)
				}
				continue
			}
			*p = v
			secretsWritten[k] = v
		}
	}
}

// configForDisk returns c as it should be written to config.json: with each server's
// passwords moved to the credential store when that works, or left in place when not.
func configForDisk(c Config) Config {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	out := c
	out.Servers = make([]ServerConfig, len(c.Servers))
	copy(out.Servers, c.Servers)
	usable := secretStoreUsable()
	seen := map[string]bool{}
	for i := range out.Servers {
		sv := &out.Servers[i]
		seen[sv.ID] = true
		if !usable || sv.ID == "" {
			// the store is unavailable right now: keep pointing at it (so the passwords come
			// back next time) unless a password was typed in meanwhile, which then wins
			empty := true
			for _, p := range secretFields(sv) {
				empty = empty && *p == ""
			}
			sv.SecretsStored = secretsUnread[sv.ID] && empty
			continue
		}
		ok := true
		for field, p := range secretFields(sv) {
			k := secretKey(sv.ID, field)
			if prev, had := secretsWritten[k]; had && prev == *p {
				continue
			}
			var err error
			if *p == "" {
				err = secrets.Delete(k)
				if errors.Is(err, errSecretNotFound) {
					err = nil
				}
			} else {
				err = secrets.Set(k, *p)
			}
			if err != nil {
				log.Printf("%s: saving %s to the credential store: %v", sv.Name, field, err)
				ok = false
				break
			}
			if *p == "" {
				delete(secretsWritten, k)
			} else {
				secretsWritten[k] = *p
			}
		}
		if !ok {
			sv.SecretsStored = false // this server's passwords stay in config.json
			continue
		}
		for _, p := range secretFields(sv) {
			*p = ""
		}
		sv.SecretsStored = true
		secretsByServer[sv.ID] = true
	}
	// servers that were removed: clear their entries too
	for id := range secretsByServer {
		if seen[id] {
			continue
		}
		for field := range secretFields(&ServerConfig{}) {
			k := secretKey(id, field)
			secrets.Delete(k)
			delete(secretsWritten, k)
		}
		delete(secretsByServer, id)
	}
	return out
}

// ---------- tests ----------

type memSecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSecrets) Get(k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[k]
	if !ok {
		return "", errSecretNotFound
	}
	return v, nil
}
func (s *memSecrets) Set(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
	return nil
}
func (s *memSecrets) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[k]; !ok {
		return errSecretNotFound
	}
	delete(s.m, k)
	return nil
}

// useSecretBackend swaps the credential store (tests).
func useSecretBackend(b secretBackend) {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	secrets, secretsUsable = b, nil
	secretsWritten = map[string]string{}
	secretsByServer = map[string]bool{}
	secretsUnread = map[string]bool{}
}
