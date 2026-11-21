package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raven-clown/bridge/forge"
)

var (
	repo = forge.RepoID{Host: "github.com", Owner: "a.b", Name: "c-d"}
	item = forge.ItemID{Repo: repo, Number: 7}
	at   = time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestOpenCreatesDirs(t *testing.T) {
	_, dir := open(t)
	for _, sub := range []string{"", "cache", "drafts", "meta"} {
		if fi, err := os.Stat(filepath.Join(dir, sub)); err != nil || !fi.IsDir() {
			t.Errorf("%q: %v", sub, err)
		}
	}
}

func TestSeenRoundTrip(t *testing.T) {
	s, dir := open(t)
	if seen, ans := s.Seen(item); seen != nil || ans != nil {
		t.Fatal("expected no marks")
	}
	if err := s.MarkSeen(item, at); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAnswered(item, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen, ans := s2.Seen(item)
	if seen == nil || !seen.Equal(at) || ans == nil || !ans.Equal(at.Add(time.Minute)) {
		t.Fatalf("seen %v answered %v", seen, ans)
	}
}

func TestMarkAnsweredLeavesSeen(t *testing.T) {
	s, _ := open(t)
	if err := s.MarkAnswered(item, at); err != nil {
		t.Fatal(err)
	}
	if seen, ans := s.Seen(item); seen != nil || ans == nil {
		t.Fatalf("seen %v answered %v", seen, ans)
	}
}

func TestAtomicWriteLeavesNoTemp(t *testing.T) {
	s, dir := open(t)
	if err := s.MarkSeen(item, at); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCache("inbox", []string{"x"}, at); err != nil {
		t.Fatal(err)
	}
	_ = filepath.Walk(dir, func(p string, _ os.FileInfo, _ error) error {
		if strings.HasSuffix(p, ".tmp") {
			t.Errorf("temp file left: %s", p)
		}
		return nil
	})
}

func TestLoadCacheMissing(t *testing.T) {
	s, _ := open(t)
	var v []string
	if _, err := s.LoadCache("inbox", &v); !errors.Is(err, ErrNoCache) {
		t.Fatalf("got %v", err)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	s, _ := open(t)
	if err := s.SaveCache("inbox", []string{"a"}, at); err != nil {
		t.Fatal(err)
	}
	var v []string
	got, err := s.LoadCache("inbox", &v)
	if err != nil || !got.Equal(at) || len(v) != 1 || v[0] != "a" {
		t.Fatalf("at %v v %v err %v", got, v, err)
	}
}

func TestReplyDraftRoundTrip(t *testing.T) {
	s, _ := open(t)
	if err := s.SaveReplyDraft(item, "C1", "thanks"); err != nil {
		t.Fatal(err)
	}
	to, body, ok, err := s.LoadReplyDraft(item)
	if err != nil || !ok || to != "C1" || body != "thanks" {
		t.Fatalf("to %q body %q ok %v err %v", to, body, ok, err)
	}
	if err := s.DeleteReplyDraft(item); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := s.LoadReplyDraft(item); ok || err != nil {
		t.Fatalf("after delete ok %v err %v", ok, err)
	}
	if err := s.DeleteReplyDraft(item); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestMetaKeyIsFilesystemSafe(t *testing.T) {
	s, dir := open(t)
	meta := forge.RepoMeta{Labels: []forge.Label{{Name: "bug"}}, ProjectsErr: forge.ErrProjectsScope}
	if err := s.SaveMeta(repo, meta); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "meta"))
	if len(entries) != 1 || strings.ContainsAny(entries[0].Name(), `/\:`) {
		t.Fatalf("entries %v", entries)
	}
	got, ok, err := s.LoadMeta(repo)
	if err != nil || !ok || len(got.Labels) != 1 || !errors.Is(got.ProjectsErr, forge.ErrProjectsScope) {
		t.Fatalf("got %+v ok %v err %v", got, ok, err)
	}
	if _, ok, _ := s.LoadMeta(forge.RepoID{Host: "github.com", Owner: "x", Name: "y"}); ok {
		t.Fatal("unexpected meta for unknown repo")
	}
}
