package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

func ConfigPath() (string, error) {
	home, _ := os.UserHomeDir()
	return configPath(runtime.GOOS, os.Getenv, home)
}

func StateDir() (string, error) {
	home, _ := os.UserHomeDir()
	return stateDir(runtime.GOOS, os.Getenv, home)
}

func configPath(goos string, env func(string) string, home string) (string, error) {
	if p := env("BRIDGE_CONFIG"); p != "" {
		return p, nil
	}
	if goos == "windows" {
		base := env("APPDATA")
		if base == "" {
			return "", errors.New("APPDATA is not set")
		}
		return filepath.Join(base, "bridge", "config.yaml"), nil
	}
	if base := env("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "bridge", "config.yaml"), nil
	}
	if home == "" {
		return "", errors.New("cannot find the home directory")
	}
	return filepath.Join(home, ".config", "bridge", "config.yaml"), nil
}

func stateDir(goos string, env func(string) string, home string) (string, error) {
	if p := env("BRIDGE_STATE_DIR"); p != "" {
		return p, nil
	}
	if goos == "windows" {
		base := env("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA is not set")
		}
		return filepath.Join(base, "bridge"), nil
	}
	if base := env("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "bridge"), nil
	}
	if home == "" {
		return "", errors.New("cannot find the home directory")
	}
	return filepath.Join(home, ".local", "state", "bridge"), nil
}
