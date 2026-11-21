// Package config loads bridge's YAML config and applies BRIDGE_* overrides.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/raven-clown/bridge/forge"
)

type Config struct {
	Roots          []string          `yaml:"roots"`
	Depth          int               `yaml:"depth"`
	RefreshSeconds int               `yaml:"refresh_seconds"`
	Auth           AuthConfig        `yaml:"auth"`
	Repos          []string          `yaml:"repos"`
	Policy         map[string]Policy `yaml:"policy"`
	Mute           Mute              `yaml:"mute"`
	Confirm        Confirm           `yaml:"confirm"`
}

type AuthConfig struct {
	Source string `yaml:"source"`
}

type Mute struct {
	Repos   []string `yaml:"repos"`
	Authors []string `yaml:"authors"`
	Kinds   []string `yaml:"kinds"`
}

type Confirm struct {
	Comment bool `yaml:"comment"`
	Triage  bool `yaml:"triage"`
	Merge   bool `yaml:"merge"`
	Release bool `yaml:"release"`
	Review  bool `yaml:"review"`
	Close   bool `yaml:"close"`
}

var (
	authSources  = []string{"keyring", "env", "device_flow"}
	mergeMethods = []string{"squash", "merge", "rebase"}
	bumps        = []string{"minor", "patch", "prompt"}
)

func defaults() Config {
	return Config{
		Depth:          2,
		RefreshSeconds: 90,
		Auth:           AuthConfig{Source: "keyring"},
		Confirm:        Confirm{Merge: true, Release: true, Review: true, Close: true},
	}
}

// Load reads path, or returns defaults when it does not exist, then applies
// env overrides and validates.
func Load(path string, env func(string) string) (Config, error) {
	c := defaults()
	data, err := os.ReadFile(path) //nolint:gosec // the user names their own config file

	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return Config{}, err
	default:
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
	}

	def := builtinPolicy()
	if fp, ok := c.Policy["default"]; ok {
		mergePolicy(&def, fp)
	}
	if c.Policy == nil {
		c.Policy = map[string]Policy{}
	}
	c.Policy["default"] = def

	if err := applyEnv(reflect.ValueOf(&c).Elem(), "", env); err != nil {
		return Config{}, err
	}
	for i, r := range c.Roots {
		c.Roots[i] = filepath.Clean(filepath.FromSlash(r))
	}
	if err := Validate(c); err != nil {
		return Config{}, err
	}
	return c, nil
}

func envName(prefix, tag string) string {
	if prefix == "" {
		return "BRIDGE_" + strings.ToUpper(tag)
	}
	return prefix + "__" + strings.ToUpper(tag)
}

func applyEnv(v reflect.Value, prefix string, env func(string) string) error {
	t := v.Type()
	for i := range t.NumField() {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		name := envName(prefix, tag)
		fv := v.Field(i)
		switch fv.Kind() {
		case reflect.Struct:
			if err := applyEnv(fv, name, env); err != nil {
				return err
			}
		case reflect.Map:
			for _, k := range fv.MapKeys() {
				elem := reflect.New(fv.Type().Elem()).Elem()
				elem.Set(fv.MapIndex(k))
				if err := applyEnv(elem, envName(name, k.String()), env); err != nil {
					return err
				}
				fv.SetMapIndex(k, elem)
			}
		default:
			if s := env(name); s != "" {
				if err := setLeaf(fv, s); err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
			}
		}
	}
	return nil
}

func setLeaf(v reflect.Value, s string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Int:
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("want an integer, got %q", s)
		}
		v.SetInt(int64(n))
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("want true or false, got %q", s)
		}
		v.SetBool(b)
	case reflect.Slice:
		parts := strings.Split(s, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		v.Set(reflect.ValueOf(parts))
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		if err := setLeaf(p.Elem(), s); err != nil {
			return err
		}
		v.Set(p)
	default:
		return fmt.Errorf("cannot set %s from the environment", v.Kind())
	}
	return nil
}

func Validate(c Config) error {
	var errs []error
	bad := func(field, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", field, fmt.Sprintf(format, args...)))
	}
	if c.Depth < 1 {
		bad("depth", "must be at least 1, got %d", c.Depth)
	}
	if c.RefreshSeconds < 10 {
		bad("refresh_seconds", "must be at least 10, got %d", c.RefreshSeconds)
	}
	if !slices.Contains(authSources, c.Auth.Source) {
		bad("auth.source", "must be one of %s, got %q", strings.Join(authSources, ", "), c.Auth.Source)
	}
	for _, r := range c.Repos {
		if _, err := forge.ParseRepoID(r); err != nil {
			bad("repos", "%v", err)
		}
	}
	for _, k := range c.Mute.Kinds {
		if _, ok := forge.ParseInboxKind(k); !ok {
			bad("mute.kinds", "unknown kind %q", k)
		}
	}
	for name, p := range c.Policy {
		if p.Merge.Method != "" && !slices.Contains(mergeMethods, p.Merge.Method) {
			bad("policy."+name+".merge.method", "must be one of %s, got %q", strings.Join(mergeMethods, ", "), p.Merge.Method)
		}
		if p.Release.Bump != "" && !slices.Contains(bumps, p.Release.Bump) {
			bad("policy."+name+".release.bump", "must be one of %s, got %q", strings.Join(bumps, ", "), p.Release.Bump)
		}
	}
	return errors.Join(errs...)
}
