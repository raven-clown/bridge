// Package auth finds a forge token: environment, OS keyring, then the
// GitHub device flow.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/raven-clown/bridge/config"
)

const (
	Service = "bridge"
	Scopes  = "repo workflow read:org read:project project"
)

var ErrNoToken = errors.New("no token found: run `bridge auth login`, or set BRIDGE_TOKEN")

func Token(ctx context.Context, cfg config.AuthConfig, host string, env func(string) string, kr Keyring, df *DeviceFlow) (string, error) {
	for _, name := range []string{"BRIDGE_TOKEN", "GITHUB_TOKEN"} {
		if t := env(name); t != "" {
			return t, nil
		}
	}
	if cfg.Source == "env" {
		return "", ErrNoToken
	}
	if cfg.Source != "device_flow" {
		t, err := kr.Get(Service, host)
		switch {
		case err == nil && t != "":
			return t, nil
		case err != nil && !errors.Is(err, ErrNotFound):
			return "", fmt.Errorf("read keyring: %w", err)
		}
	}
	if df == nil {
		return "", ErrNoToken
	}
	t, err := df.Run(ctx, strings.Fields(Scopes))
	if err != nil {
		return "", err
	}
	if err := kr.Set(Service, host, t); err != nil {
		return "", errors.New("signed in, but saving the token to the keyring failed: " + redact(err.Error(), t))
	}
	return t, nil
}

func redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "***")
}
