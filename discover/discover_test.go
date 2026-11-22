package discover

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/raven-clown/bridge/forge"
)

func TestParseRemote(t *testing.T) {
	tests := []struct {
		url    string
		want   string
		wantOK bool
	}{
		{"https://github.com/o/n.git", "o/n", true},
		{"https://user@github.com/o/n", "o/n", true},
		{"git@github.com:o/n.git", "o/n", true},
		{"ssh://git@github.com/o/n", "o/n", true},
		{"https://gitlab.com/o/n.git", "", false},
		{"https://github.com/o", "", false},
		{"D:/other/repo", "", false},
		{"/srv/git/repo.git", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseRemote(tt.url)
		if ok != tt.wantOK || (ok && got.String() != tt.want) || (ok && got.Host != "github.com") {
			t.Errorf("ParseRemote(%q) = %v, %v", tt.url, got, ok)
		}
	}
	got, ok := ParseRemoteHosts("git@ghe.corp:team/tool.git", []string{"ghe.corp"})
	if !ok || got != (forge.RepoID{Host: "ghe.corp", Owner: "team", Name: "tool"}) {
		t.Errorf("enterprise: %v %v", got, ok)
	}
}

func initRepo(t *testing.T, dir, remote string) *git.Repository {
	t.Helper()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if remote != "" {
		if _, err := r.CreateRemote(&gitconfig.RemoteConfig{Name: "origin", URLs: []string{remote}}); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func commit(t *testing.T, r *git.Repository, dir, file string) plumbing.Hash {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(file); err != nil {
		t.Fatal(err)
	}
	h, err := wt.Commit(file, &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func paths(repos []LocalRepo, root string) []string {
	var out []string
	for _, r := range repos {
		rel, _ := filepath.Rel(root, r.Path)
		out = append(out, filepath.ToSlash(rel))
	}
	slices.Sort(out)
	return out
}

func TestScanFindsReposToDepth(t *testing.T) {
	root := t.TempDir()
	initRepo(t, filepath.Join(root, "a"), "https://github.com/o/a.git")
	initRepo(t, filepath.Join(root, "b", "c"), "git@github.com:o/c.git")
	initRepo(t, filepath.Join(root, "d", "e", "f"), "https://github.com/o/f.git")
	if err := os.MkdirAll(filepath.Join(root, ".hidden", "g"), 0o700); err != nil {
		t.Fatal(err)
	}
	initRepo(t, filepath.Join(root, ".hidden", "g"), "https://github.com/o/g.git")

	repos, err := Scan(context.Background(), []string{root}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths(repos, root), ","); got != "a,b/c" {
		t.Fatalf("found %s", got)
	}
	for _, r := range repos {
		if r.Repo == nil {
			t.Errorf("%s: remote %q not mapped", r.Path, r.Remote)
		}
	}
}

func TestScanUnmappedRemoteIsListed(t *testing.T) {
	root := t.TempDir()
	initRepo(t, filepath.Join(root, "x"), "https://gitlab.com/o/x.git")
	repos, err := Scan(context.Background(), []string{root}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Repo != nil || repos[0].Remote != "https://gitlab.com/o/x.git" {
		t.Fatalf("got %+v", repos)
	}
}

func TestScanNoRemote(t *testing.T) {
	root := t.TempDir()
	initRepo(t, filepath.Join(root, "x"), "")
	repos, err := Scan(context.Background(), []string{root}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Repo != nil || repos[0].Remote != "" {
		t.Fatalf("got %+v", repos)
	}
}

func TestScanMissingRootIsSkipped(t *testing.T) {
	repos, err := Scan(context.Background(), []string{filepath.Join(t.TempDir(), "gone")}, 2)
	if err != nil || len(repos) != 0 {
		t.Fatalf("repos %v err %v", repos, err)
	}
}

func TestScanWindowsStylePath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("backslash roots only apply on Windows")
	}
	root := t.TempDir()
	initRepo(t, filepath.Join(root, "a"), "https://github.com/o/a.git")
	repos, err := Scan(context.Background(), []string{strings.ReplaceAll(root, "/", `\`) + `\`}, 1)
	if err != nil || len(repos) != 1 {
		t.Fatalf("repos %v err %v", repos, err)
	}
}

func TestScanListsWorktrees(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	initRepo(t, main, "https://github.com/o/m.git")
	wt := filepath.Join(t.TempDir(), "feature")
	meta := filepath.Join(main, ".git", "worktrees", "feature")
	if err := os.MkdirAll(meta, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "gitdir"), []byte(filepath.ToSlash(filepath.Join(wt, ".git"))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repos, err := Scan(context.Background(), []string{root}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || len(repos[0].Worktrees) != 1 || repos[0].Worktrees[0] != wt {
		t.Fatalf("worktrees = %v, want [%s]", repos[0].Worktrees, wt)
	}
}

func TestStatusDirtyAheadBehind(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r")
	r := initRepo(t, dir, "https://github.com/o/r.git")
	c1 := commit(t, r, dir, "one.txt")
	c2 := commit(t, r, dir, "two.txt")
	head, _ := r.Head()
	branch := head.Name().Short()
	track := plumbing.NewRemoteReferenceName("origin", branch)

	if err := r.Storer.SetReference(plumbing.NewHashReference(track, c1)); err != nil {
		t.Fatal(err)
	}
	lr := LocalRepo{Path: dir}
	if err := Status(context.Background(), &lr); err != nil {
		t.Fatal(err)
	}
	if lr.Dirty || lr.Ahead != 1 || lr.Behind != 0 || lr.Head != branch {
		t.Fatalf("after local commit: %+v", lr)
	}

	if err := r.Storer.SetReference(plumbing.NewHashReference(track, c2)); err != nil {
		t.Fatal(err)
	}
	if err := r.Storer.SetReference(plumbing.NewHashReference(head.Name(), c1)); err != nil {
		t.Fatal(err)
	}
	wt, _ := r.Worktree()
	if err := wt.Reset(&git.ResetOptions{Commit: c1, Mode: git.HardReset}); err != nil {
		t.Fatal(err)
	}
	if err := Status(context.Background(), &lr); err != nil {
		t.Fatal(err)
	}
	if lr.Ahead != 0 || lr.Behind != 1 {
		t.Fatalf("behind: %+v", lr)
	}

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Status(context.Background(), &lr); err != nil {
		t.Fatal(err)
	}
	if !lr.Dirty {
		t.Fatal("want dirty")
	}
}
