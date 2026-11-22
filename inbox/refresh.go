package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/discover"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/store"
)

type Problem struct {
	Source  string
	Repo    *forge.RepoID
	Err     error
	RetryAt *time.Time
}

type problemJSON struct {
	Source  string        `json:"source"`
	Repo    *forge.RepoID `json:"repo,omitempty"`
	Error   string        `json:"error"`
	RetryAt *time.Time    `json:"retry_at,omitempty"`
}

func (p Problem) MarshalJSON() ([]byte, error) {
	j := problemJSON{Source: p.Source, Repo: p.Repo, RetryAt: p.RetryAt}
	if p.Err != nil {
		j.Error = p.Err.Error()
	}
	return json.Marshal(j)
}

func (p *Problem) UnmarshalJSON(data []byte) error {
	var j problemJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	*p = Problem{Source: j.Source, Repo: j.Repo, RetryAt: j.RetryAt}
	if j.Error != "" {
		p.Err = errors.New(j.Error)
	}
	return nil
}

type Result struct {
	Items     []Item    `json:"items"`
	Problems  []Problem `json:"problems,omitempty"`
	Truncated []string  `json:"truncated,omitempty"`
	At        time.Time `json:"at"`
}

type Refresher struct {
	Forge  forge.Forge
	Store  *store.Store
	Config config.Config
	Me     string
	Repos  []discover.LocalRepo
	Extra  []forge.RepoID
	Now    func() time.Time
}

const cacheName = "inbox"

// watched returns the repositories to ask for incoming items (local clones
// you can write to, plus the extra ones from config), every local path, and
// the writable local repositories whose branches to list. When the
// permission check fails it falls back to repositories you own.
func (r *Refresher) watched(ctx context.Context) (incoming []forge.RepoID, local map[forge.RepoID]string, mine []forge.RepoID, err error) {
	local = map[forge.RepoID]string{}
	var clones []forge.RepoID
	for _, lr := range r.Repos {
		if lr.Repo == nil {
			continue
		}
		if _, ok := local[*lr.Repo]; !ok {
			local[*lr.Repo] = lr.Path
			clones = append(clones, *lr.Repo)
		}
	}
	if len(clones) > 0 {
		mine, err = r.Forge.Writable(ctx, clones)
		if err != nil {
			mine = slices.DeleteFunc(slices.Clone(clones), func(id forge.RepoID) bool { return !strings.EqualFold(id.Owner, r.Me) })
		}
	}
	incoming = slices.Clone(mine)
	for _, e := range r.Extra {
		if !slices.Contains(incoming, e) {
			incoming = append(incoming, e)
		}
	}
	return incoming, local, mine, err
}

func (r *Refresher) Refresh(ctx context.Context) Result {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	repos, local, mine, permErr := r.watched(ctx)

	var (
		mu  sync.Mutex
		res Result
		in  = Input{Me: r.Me, Branches: map[forge.RepoID][]forge.Branch{}, Local: local}
	)
	if permErr != nil {
		res.Problems = append(res.Problems, Problem{Source: "permissions", Err: permErr})
	}
	fail := func(source string, repo *forge.RepoID, err error) {
		p := Problem{Source: source, Repo: repo, Err: err}
		if rl, ok := errors.AsType[*forge.RateLimitedError](err); ok {
			p.RetryAt = &rl.RetryAt
		}
		mu.Lock()
		res.Problems = append(res.Problems, p)
		mu.Unlock()
	}
	trunc := func(source string, t bool) {
		if t {
			mu.Lock()
			res.Truncated = append(res.Truncated, source)
			mu.Unlock()
		}
	}

	var g errgroup.Group
	g.SetLimit(8)
	g.Go(func() error {
		l, err := r.Forge.ListMyPRs(ctx, nil)
		if err != nil {
			fail("my-prs", nil, err)
			return nil
		}
		trunc("my-prs", l.Truncated)
		mu.Lock()
		in.MyPRs = l.Items
		mu.Unlock()
		return nil
	})
	if len(repos) > 0 {
		g.Go(func() error {
			l, err := r.Forge.ListIncoming(ctx, repos, nil)
			if err != nil {
				fail("incoming", nil, err)
				return nil
			}
			trunc("incoming", l.Truncated)
			mu.Lock()
			in.Incoming = l.Items
			mu.Unlock()
			return nil
		})
	}
	g.Go(func() error {
		l, err := r.Forge.ListReviewRequests(ctx)
		if err != nil {
			fail("review-requests", nil, err)
			return nil
		}
		trunc("review-requests", l.Truncated)
		mu.Lock()
		in.ReviewRequests = l.Items
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		l, err := r.Forge.ListMentions(ctx, nil)
		if err != nil {
			fail("mentions", nil, err)
			return nil
		}
		trunc("mentions", l.Truncated)
		mu.Lock()
		in.Mentions = l.Items
		mu.Unlock()
		return nil
	})
	for _, repo := range mine {
		g.Go(func() error {
			b, err := r.Forge.ListBranches(ctx, repo)
			if err != nil {
				fail("branches", &repo, err)
				return nil
			}
			mu.Lock()
			in.Branches[repo] = b
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	in.Seen = func(forge.ItemID) (*time.Time, *time.Time) { return nil, nil }
	if r.Store != nil {
		in.Seen = r.Store.Seen
	}
	in.Muted = r.Config.Muted
	res.Items = Build(in)
	res.At = now()
	slices.SortStableFunc(res.Problems, func(a, b Problem) int { return strings.Compare(a.Source, b.Source) })

	if r.Store != nil {
		if err := r.Store.SaveCache(cacheName, res, res.At); err != nil {
			res.Problems = append(res.Problems, Problem{Source: "cache", Err: err})
		}
	}
	return res
}

func (r *Refresher) Cached() (Result, bool) {
	if r.Store == nil {
		return Result{}, false
	}
	var res Result
	at, err := r.Store.LoadCache(cacheName, &res)
	if err != nil {
		return Result{}, false
	}
	res.At = at
	return res, true
}
