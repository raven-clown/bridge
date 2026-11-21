// Package store keeps bridge's local state: seen marks, caches and drafts.
// Everything here can be deleted without losing anything on the forge.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/raven-clown/bridge/forge"
)

var ErrNoCache = errors.New("no cache")

type Store struct {
	dir  string
	mu   sync.Mutex
	seen map[string]mark
}

func Open(dir string) (*Store, error) {
	for _, sub := range []string{"", "cache", "drafts", "meta"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	s := &Store{dir: dir, seen: map[string]mark{}}
	if err := s.readJSON("seen.json", &s.seen); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("seen.json: %w", err)
	}
	return s, nil
}

func (s *Store) Dir() string { return s.dir }

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func safe(parts ...string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "__"
		}
		out += unsafe.ReplaceAllString(p, "_")
	}
	return out
}

func repoKey(r forge.RepoID) string { return safe(r.Host, r.Owner, r.Name) }

func itemKey(id forge.ItemID) string {
	return safe(id.Repo.Host, id.Repo.Owner, id.Repo.Name, fmt.Sprint(id.Number))
}

func (s *Store) readJSON(rel string, v any) error {
	data, err := os.ReadFile(filepath.Join(s.dir, rel)) //nolint:gosec // rel is built from sanitized keys under the state dir
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSON writes through a temp file and a rename so a crash never leaves
// a half-written file behind.
func (s *Store) writeJSON(rel string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, rel)
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
