package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/raven-clown/bridge/forge"
)

type envelope struct {
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
}

func (s *Store) SaveCache(name string, v any, at time.Time) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.writeJSON(filepath.Join("cache", safe(name)+".json"), envelope{At: at, Data: data})
}

func (s *Store) LoadCache(name string, v any) (time.Time, error) {
	var e envelope
	if err := s.readJSON(filepath.Join("cache", safe(name)+".json"), &e); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return time.Time{}, ErrNoCache
		}
		return time.Time{}, err
	}
	return e.At, json.Unmarshal(e.Data, v)
}

type metaFile struct {
	Meta        forge.RepoMeta `json:"meta"`
	ProjectsErr string         `json:"projects_err,omitempty"`
}

func (s *Store) SaveMeta(repo forge.RepoID, meta forge.RepoMeta) error {
	mf := metaFile{Meta: meta}
	if meta.ProjectsErr != nil {
		mf.ProjectsErr = meta.ProjectsErr.Error()
	}
	return s.writeJSON(filepath.Join("meta", repoKey(repo)+".json"), mf)
}

func (s *Store) LoadMeta(repo forge.RepoID) (forge.RepoMeta, bool, error) {
	var mf metaFile
	if err := s.readJSON(filepath.Join("meta", repoKey(repo)+".json"), &mf); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return forge.RepoMeta{}, false, nil
		}
		return forge.RepoMeta{}, false, err
	}
	switch mf.ProjectsErr {
	case "":
	case forge.ErrProjectsScope.Error():
		mf.Meta.ProjectsErr = forge.ErrProjectsScope
	default:
		mf.Meta.ProjectsErr = errors.New(mf.ProjectsErr)
	}
	return mf.Meta, true, nil
}
