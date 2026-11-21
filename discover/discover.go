// Package discover finds local git repositories under the configured roots
// and maps their remotes to forge repositories.
package discover

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/raven-clown/bridge/forge"
)

type LocalRepo struct {
	Path      string        `json:"path"`
	Remote    string        `json:"remote,omitempty"`
	Repo      *forge.RepoID `json:"repo,omitempty"`
	Head      string        `json:"head,omitempty"`
	Dirty     bool          `json:"dirty,omitempty"`
	Ahead     int           `json:"ahead,omitempty"`
	Behind    int           `json:"behind,omitempty"`
	Worktrees []string      `json:"worktrees,omitempty"`
	ScannedAt time.Time     `json:"scanned_at"`
}

func ParseRemote(u string) (forge.RepoID, bool) {
	return ParseRemoteHosts(u, nil)
}

// ParseRemoteHosts maps a remote URL on github.com or one of hosts.
func ParseRemoteHosts(u string, hosts []string) (forge.RepoID, bool) {
	var host, p string
	if strings.Contains(u, "://") {
		pu, err := url.Parse(u)
		if err != nil {
			return forge.RepoID{}, false
		}
		host, p = pu.Hostname(), pu.Path
	} else {
		at := strings.Index(u, "@")
		colon := strings.Index(u, ":")
		if at < 0 || colon < at {
			return forge.RepoID{}, false
		}
		host, p = u[at+1:colon], u[colon+1:]
	}
	host = strings.ToLower(host)
	if host != forge.DefaultHost && !slices.Contains(hosts, host) {
		return forge.RepoID{}, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.Trim(p, "/"), ".git"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return forge.RepoID{}, false
	}
	return forge.RepoID{Host: host, Owner: parts[0], Name: parts[1]}, true
}

func Scan(ctx context.Context, roots []string, depth int) ([]LocalRepo, error) {
	var out []LocalRepo
	for _, root := range roots {
		root = filepath.Clean(root)
		if err := walk(ctx, root, depth, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func walk(ctx context.Context, dir string, left int, out *[]LocalRepo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		if lr, ok := open(dir); ok {
			*out = append(*out, lr)
		}
		return nil
	}
	if left == 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" {
			continue
		}
		if err := walk(ctx, filepath.Join(dir, e.Name()), left-1, out); err != nil {
			return err
		}
	}
	return nil
}

func openGit(dir string) (*git.Repository, error) {
	return git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
}

func open(dir string) (LocalRepo, bool) {
	r, err := openGit(dir)
	if err != nil {
		return LocalRepo{}, false
	}
	lr := LocalRepo{Path: dir, ScannedAt: time.Now()}
	if remotes, err := r.Remotes(); err == nil && len(remotes) > 0 {
		slices.SortFunc(remotes, func(a, b *git.Remote) int {
			switch {
			case a.Config().Name == "origin":
				return -1
			case b.Config().Name == "origin":
				return 1
			}
			return strings.Compare(a.Config().Name, b.Config().Name)
		})
		if urls := remotes[0].Config().URLs; len(urls) > 0 {
			lr.Remote = urls[0]
			if id, ok := ParseRemote(lr.Remote); ok {
				lr.Repo = &id
			}
		}
	}
	if head, err := r.Head(); err == nil {
		lr.Head = headName(head)
	}
	lr.Worktrees = worktrees(dir)
	return lr, true
}

func headName(ref *plumbing.Reference) string {
	if ref.Name().IsBranch() {
		return ref.Name().Short()
	}
	return ref.Hash().String()[:7]
}

func worktrees(dir string) []string {
	entries, err := os.ReadDir(filepath.Join(dir, ".git", "worktrees"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, ".git", "worktrees", e.Name(), "gitdir")) //nolint:gosec // inside the repository's own .git
		if err != nil {
			continue
		}
		out = append(out, filepath.Dir(filepath.FromSlash(strings.TrimSpace(string(data)))))
	}
	return out
}

// Status fills Dirty, Ahead and Behind against origin/<current branch>,
// falling back to origin/HEAD. It reads the whole worktree, so it is slow
// on large repositories and runs only on demand.
func Status(ctx context.Context, lr *LocalRepo) error {
	r, err := openGit(lr.Path)
	if err != nil {
		return err
	}
	wt, err := r.Worktree()
	if err != nil {
		return err
	}
	st, err := wt.Status()
	if err != nil {
		return err
	}
	lr.Dirty = !st.IsClean()

	head, err := r.Head()
	if err != nil {
		return nil
	}
	lr.Head = headName(head)
	remote := upstream(r, head)
	if remote == nil {
		lr.Ahead, lr.Behind = 0, 0
		return nil
	}
	lr.Ahead, lr.Behind, err = aheadBehind(ctx, r, head.Hash(), remote.Hash())
	return err
}

func upstream(r *git.Repository, head *plumbing.Reference) *plumbing.Reference {
	if head.Name().IsBranch() {
		if ref, err := r.Reference(plumbing.NewRemoteReferenceName("origin", head.Name().Short()), true); err == nil {
			return ref
		}
	}
	if ref, err := r.Reference(plumbing.NewRemoteHEADReferenceName("origin"), true); err == nil {
		return ref
	}
	return nil
}

func aheadBehind(ctx context.Context, r *git.Repository, local, remote plumbing.Hash) (int, int, error) {
	if local == remote {
		return 0, 0, nil
	}
	lc, err := r.CommitObject(local)
	if err != nil {
		return 0, 0, err
	}
	rc, err := r.CommitObject(remote)
	if err != nil {
		return 0, 0, err
	}
	bases, err := lc.MergeBase(rc)
	if err != nil {
		return 0, 0, err
	}
	var ignore []plumbing.Hash
	for _, b := range bases {
		ignore = append(ignore, b.Hash)
	}
	count := func(c *object.Commit) (int, error) {
		n := 0
		err := object.NewCommitPreorderIter(c, nil, ignore).ForEach(func(*object.Commit) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			n++
			return nil
		})
		return n, err
	}
	ahead, err := count(lc)
	if err != nil {
		return 0, 0, err
	}
	behind, err := count(rc)
	return ahead, behind, err
}
