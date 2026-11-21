package auth

import (
	"errors"

	"github.com/zalando/go-keyring"
)

var ErrNotFound = errors.New("not in keyring")

type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
	Delete(service, user string) error
}

type osKeyring struct{}

// OSKeyring uses Windows Credential Manager, macOS Keychain or Secret Service.
func OSKeyring() Keyring { return osKeyring{} }

func (osKeyring) Get(service, user string) (string, error) {
	s, err := keyring.Get(service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return s, err
}

func (osKeyring) Set(service, user, secret string) error { return keyring.Set(service, user, secret) }

func (osKeyring) Delete(service, user string) error {
	err := keyring.Delete(service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
