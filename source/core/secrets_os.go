//go:build !android

package core

import (
	"errors"

	"github.com/zalando/go-keyring"
)

type osKeyring struct{}

func osSecretBackend() secretBackend { return osKeyring{} }

func (osKeyring) Get(k string) (string, error) {
	v, err := keyring.Get(keyringService, k)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errSecretNotFound
	}
	return v, err
}
func (osKeyring) Set(k, v string) error { return keyring.Set(keyringService, k, v) }
func (osKeyring) Delete(k string) error {
	err := keyring.Delete(keyringService, k)
	if errors.Is(err, keyring.ErrNotFound) {
		return errSecretNotFound
	}
	return err
}
