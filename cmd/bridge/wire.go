package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/raven-clown/bridge/auth"
	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/discover"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/github"
	"github.com/raven-clown/bridge/inbox"
	"github.com/raven-clown/bridge/store"
)

const reposCacheTTL = 24 * time.Hour

// wireOptions lets tests swap the forge, paths, env and clock.
type wireOptions struct {
	forge      forge.Forge
	configPath string
	stateDir   string
	env        func(string) string
	now        func() time.Time
	rescan     bool
}

type app struct {
	cfg   config.Config
	forge forge.Forge
	store *store.Store
	me    string
	repos []discover.LocalRepo
	extra []forge.RepoID
	env   func(string) string
	now   func() time.Time
}

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit status %d", e.code)
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

func (o wireOptions) withDefaults() wireOptions {
	if o.env == nil {
		o.env = os.Getenv
	}
	if o.now == nil {
		o.now = time.Now
	}
	return o
}

func loadConfig(o wireOptions) (config.Config, error) {
	path := o.configPath
	if path == "" {
		p, err := config.ConfigPath()
		if err != nil {
			return config.Config{}, err
		}
		path = p
	}
	return config.Load(path, o.env)
}

func openStore(o wireOptions) (*store.Store, error) {
	dir := o.stateDir
	if dir == "" {
		d, err := config.StateDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	return store.Open(dir)
}

func newForge(ctx context.Context, cfg config.Config, env func(string) string) (forge.Forge, error) {
	tok, err := auth.Token(ctx, cfg.Auth, forge.DefaultHost, env, auth.OSKeyring(), nil)
	if err != nil {
		return nil, err
	}
	return github.New(forge.DefaultHost, tok, nil)
}

func wire(ctx context.Context, o wireOptions) (*app, error) {
	o = o.withDefaults()
	cfg, err := loadConfig(o)
	if err != nil {
		return nil, err
	}
	s, err := openStore(o)
	if err != nil {
		return nil, err
	}
	f := o.forge
	if f == nil {
		if f, err = newForge(ctx, cfg, o.env); err != nil {
			return nil, err
		}
	}
	me, err := whoAmI(ctx, f, s, o.now())
	if err != nil {
		return nil, err
	}
	a := &app{cfg: cfg, forge: f, store: s, me: me, env: o.env, now: o.now}
	for _, r := range cfg.Repos {
		id, err := forge.ParseRepoID(r)
		if err != nil {
			return nil, err
		}
		a.extra = append(a.extra, id)
	}
	a.repos, err = a.loadRepos(ctx, o.rescan)
	return a, err
}

// whoAmI falls back to the login cached by an earlier run, so bridge still
// starts from its cache when GitHub cannot be reached.
func whoAmI(ctx context.Context, f forge.Forge, s *store.Store, now time.Time) (string, error) {
	me, err := f.WhoAmI(ctx)
	if err == nil {
		return me.Login, s.SaveCache("me", me.Login, now)
	}
	var cached string
	if _, cerr := s.LoadCache("me", &cached); cerr == nil && cached != "" {
		return cached, nil
	}
	return "", fmt.Errorf("who am I: %w", err)
}

// reposCache records what it was scanned from, so changing roots or depth
// rescans at once instead of after the cache expires.
type reposCache struct {
	Roots []string             `json:"roots"`
	Depth int                  `json:"depth"`
	Repos []discover.LocalRepo `json:"repos"`
}

func (a *app) loadRepos(ctx context.Context, rescan bool) ([]discover.LocalRepo, error) {
	var c reposCache
	at, err := a.store.LoadCache("repos", &c)
	if err != nil && !errors.Is(err, store.ErrNoCache) {
		return nil, err
	}
	fresh := err == nil && a.now().Sub(at) < reposCacheTTL && slices.Equal(c.Roots, a.cfg.Roots) && c.Depth == a.cfg.Depth
	if fresh && !rescan {
		return c.Repos, nil
	}
	return a.scan(ctx)
}

func (a *app) scan(ctx context.Context) ([]discover.LocalRepo, error) {
	repos, err := discover.Scan(ctx, a.cfg.Roots, a.cfg.Depth)
	if err != nil {
		return nil, err
	}
	return repos, a.store.SaveCache("repos", reposCache{Roots: a.cfg.Roots, Depth: a.cfg.Depth, Repos: repos}, a.now())
}

func (a *app) refresher() *inbox.Refresher {
	return &inbox.Refresher{Forge: a.forge, Store: a.store, Config: a.cfg, Me: a.me, Repos: a.repos, Extra: a.extra, Now: a.now, Rescan: a.scan}
}
