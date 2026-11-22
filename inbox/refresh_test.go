package inbox

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/discover"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/forgetest"
	"github.com/raven-clown/bridge/store"
)

var repoB = forge.RepoID{Host: "github.com", Owner: "o", Name: "b"}

func refresher(t *testing.T, f *forgetest.Fake) *Refresher {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(t.TempDir(), "none.yaml"), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	a := repoA
	return &Refresher{
		Forge:  f,
		Store:  s,
		Config: cfg,
		Me:     "me",
		Repos:  []discover.LocalRepo{{Path: "/src/a", Repo: &a}},
		Extra:  []forge.RepoID{repoB, repoA},
		Now:    func() time.Time { return t0 },
	}
}

func fakeWithAll() *forgetest.Fake {
	f := forgetest.New()
	f.MyPRs = []forge.Item{{ID: id(1), IsPR: true, Author: forge.User{Login: "me"}}}
	f.Incoming = []forge.Item{{ID: id(2), IsPR: true}, {ID: forge.ItemID{Repo: repoB, Number: 3}}}
	f.ReviewRequests = []forge.Item{{ID: id(4), IsPR: true}}
	f.Mentions = []forge.CommentRef{{Item: id(5), Comment: "C5", Author: forge.User{Login: "x"}, At: t0}}
	f.Branches[repoA] = []forge.Branch{{Name: "feat/x", PushedAt: t0}}
	return f
}

func TestRefreshAllSources(t *testing.T) {
	f := fakeWithAll()
	res := refresher(t, f).Refresh(context.Background())
	if len(res.Problems) != 0 {
		t.Fatalf("problems: %+v", res.Problems)
	}
	got := map[forge.InboxKind]int{}
	for _, it := range res.Items {
		got[it.Kind]++
	}
	want := map[forge.InboxKind]int{forge.OwnPR: 1, forge.IncomingPR: 1, forge.IncomingIssue: 1, forge.ReviewRequest: 1, forge.Mention: 1, forge.PushedBranch: 1}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%v: got %d, want %d", k, got[k], n)
		}
	}
	calls := f.CallLog()
	for _, m := range []string{"ListMyPRs", "ListIncoming", "ListReviewRequests", "ListMentions", "ListBranches"} {
		if !slices.Contains(calls, m) {
			t.Errorf("no %s call in %v", m, calls)
		}
	}
	if !res.At.Equal(t0) {
		t.Errorf("at = %v", res.At)
	}
}

func TestRefreshPartialFailureIsProblem(t *testing.T) {
	f := fakeWithAll()
	f.Err["ListMentions"] = errors.New("boom")
	res := refresher(t, f).Refresh(context.Background())
	if len(res.Problems) != 1 || res.Problems[0].Source != "mentions" || res.Problems[0].Err.Error() != "boom" {
		t.Fatalf("problems = %+v", res.Problems)
	}
	if len(res.Items) != 5 {
		t.Fatalf("items = %d, want 5", len(res.Items))
	}
}

func TestRefreshBranchProblemNamesRepo(t *testing.T) {
	f := fakeWithAll()
	f.Err["ListBranches"] = errors.New("nope")
	res := refresher(t, f).Refresh(context.Background())
	if len(res.Problems) != 1 || res.Problems[0].Repo == nil || *res.Problems[0].Repo != repoA {
		t.Fatalf("problems = %+v", res.Problems)
	}
}

func TestRefreshRateLimited(t *testing.T) {
	f := fakeWithAll()
	retry := t0.Add(30 * time.Second)
	f.Err["ListMyPRs"] = &forge.RateLimitedError{RetryAt: retry}
	res := refresher(t, f).Refresh(context.Background())
	if len(res.Problems) != 1 || res.Problems[0].RetryAt == nil || !res.Problems[0].RetryAt.Equal(retry) {
		t.Fatalf("problems = %+v", res.Problems)
	}
}

func TestRefreshTruncated(t *testing.T) {
	f := fakeWithAll()
	f.MyPRsTrunc = true
	res := refresher(t, f).Refresh(context.Background())
	if !slices.Equal(res.Truncated, []string{"my-prs"}) {
		t.Fatalf("truncated = %v", res.Truncated)
	}
}

func TestRefreshSkipsUnmappedRepos(t *testing.T) {
	f := fakeWithAll()
	r := refresher(t, f)
	r.Repos = []discover.LocalRepo{{Path: "/src/gitlab", Remote: "https://gitlab.com/o/x"}}
	r.Extra = nil
	res := r.Refresh(context.Background())
	if slices.Contains(f.CallLog(), "ListBranches") || slices.Contains(f.CallLog(), "ListIncoming") {
		t.Fatalf("calls = %v", f.CallLog())
	}
	if len(res.Problems) != 0 {
		t.Fatalf("problems = %+v", res.Problems)
	}
}

func TestRefreshIncomingOnlyWritableRepos(t *testing.T) {
	f := fakeWithAll()
	upstream := forge.RepoID{Host: "github.com", Owner: "someone", Name: "big"}
	f.ReadOnly = map[forge.RepoID]bool{upstream: true}
	f.Incoming = append(f.Incoming, forge.Item{ID: forge.ItemID{Repo: upstream, Number: 9}, Title: "not mine"})
	r := refresher(t, f)
	a := repoA
	r.Repos = []discover.LocalRepo{{Path: "/src/a", Repo: &a}, {Path: "/src/big", Repo: &upstream}}
	r.Extra = []forge.RepoID{repoB}
	res := r.Refresh(context.Background())
	if got := f.LastIncoming(); !slices.Equal(got, []forge.RepoID{repoA, repoB}) {
		t.Fatalf("incoming asked for %v", got)
	}
	for _, it := range res.Items {
		if it.Repo == upstream && it.Kind == forge.IncomingIssue {
			t.Fatalf("read-only repo in the inbox: %+v", it)
		}
	}
	if !slices.Contains(f.CallLog(), "ListBranches") {
		t.Fatal("branches of the writable local repo were not listed")
	}
}

func TestRefreshWritableFailureIsProblem(t *testing.T) {
	f := fakeWithAll()
	f.Err["Writable"] = errors.New("nope")
	res := refresher(t, f).Refresh(context.Background())
	found := false
	for _, p := range res.Problems {
		found = found || p.Source == "permissions"
	}
	if !found {
		t.Fatalf("problems = %+v", res.Problems)
	}
}

var listSources = []string{"ListMyPRs", "ListIncoming", "ListReviewRequests", "ListMentions", "ListBranches"}

func TestRefreshKeepsItemsFromFailedSources(t *testing.T) {
	f := fakeWithAll()
	r := refresher(t, f)
	first := r.Refresh(context.Background())
	for _, m := range listSources {
		f.Err[m] = &forge.RateLimitedError{RetryAt: t0.Add(time.Minute)}
	}
	second := r.Refresh(context.Background())
	if len(second.Items) != len(first.Items) {
		t.Fatalf("items %d, want %d kept from the last refresh", len(second.Items), len(first.Items))
	}
	// Branches are not fetched again within ten minutes, so four sources fail.
	if len(second.Problems) != 4 {
		t.Fatalf("problems = %+v", second.Problems)
	}
	cached, _ := r.Cached()
	if len(cached.Items) != len(first.Items) {
		t.Fatalf("cache lost items: %d", len(cached.Items))
	}
}

func TestRefreshCarriesCacheOnFirstFailure(t *testing.T) {
	f := fakeWithAll()
	r := refresher(t, f)
	first := r.Refresh(context.Background())

	f.Err["ListMyPRs"] = errors.New("offline")
	f.MyPRs = nil
	fresh := &Refresher{Forge: f, Store: r.Store, Config: r.Config, Me: r.Me, Repos: r.Repos, Extra: r.Extra, Now: r.Now}
	got := fresh.Refresh(context.Background())
	own := 0
	for _, it := range got.Items {
		if it.Kind == forge.OwnPR {
			own++
		}
	}
	if own != 1 || len(got.Items) != len(first.Items) {
		t.Fatalf("own PRs %d, items %d want %d", own, len(got.Items), len(first.Items))
	}
}

func TestRefreshCollapsesBranchProblems(t *testing.T) {
	f := fakeWithAll()
	f.Err["ListBranches"] = errors.New("nope")
	r := refresher(t, f)
	b, c := repoB, forge.RepoID{Host: "github.com", Owner: "o", Name: "c"}
	a := repoA
	r.Repos = []discover.LocalRepo{{Path: "/a", Repo: &a}, {Path: "/b", Repo: &b}, {Path: "/c", Repo: &c}}
	res := r.Refresh(context.Background())
	n := 0
	for _, p := range res.Problems {
		if p.Source == "branches" {
			n++
			if !strings.Contains(p.Err.Error(), "3 repositories") {
				t.Errorf("err = %v", p.Err)
			}
		}
	}
	if n != 1 {
		t.Fatalf("branch problems = %d: %+v", n, res.Problems)
	}
}

func count(calls []string, name string) int {
	n := 0
	for _, c := range calls {
		if c == name {
			n++
		}
	}
	return n
}

func TestRefreshCachesWritable(t *testing.T) {
	f := fakeWithAll()
	r := refresher(t, f)
	r.Refresh(context.Background())
	r.Refresh(context.Background())
	if n := count(f.CallLog(), "Writable"); n != 1 {
		t.Fatalf("Writable called %d times", n)
	}
}

func TestRefreshBranchesEveryTenMinutes(t *testing.T) {
	f := fakeWithAll()
	r := refresher(t, f)
	clock := t0
	r.Now = func() time.Time { return clock }
	r.Refresh(context.Background())
	clock = clock.Add(5 * time.Minute)
	res := r.Refresh(context.Background())
	if n := count(f.CallLog(), "ListBranches"); n != 1 {
		t.Fatalf("ListBranches called %d times within 10 minutes", n)
	}
	pushed := 0
	for _, it := range res.Items {
		if it.Kind == forge.PushedBranch {
			pushed++
		}
	}
	if pushed != 1 {
		t.Fatalf("pushed branch lost between branch fetches: %d", pushed)
	}
	clock = clock.Add(6 * time.Minute)
	r.Refresh(context.Background())
	if n := count(f.CallLog(), "ListBranches"); n != 2 {
		t.Fatalf("ListBranches called %d times after 11 minutes", n)
	}
}

func TestRefreshRescanPicksUpNewRepos(t *testing.T) {
	f := fakeWithAll()
	r := refresher(t, f)
	r.Extra = nil
	r.Refresh(context.Background())
	b := repoB
	r.Rescan = func(context.Context) ([]discover.LocalRepo, error) {
		return []discover.LocalRepo{{Path: "/src/b", Repo: &b}}, nil
	}
	r.RefreshRescan(context.Background())
	if got := f.LastIncoming(); !slices.Contains(got, repoB) || slices.Contains(got, repoA) {
		t.Fatalf("incoming asked for %v", got)
	}
	if n := count(f.CallLog(), "Writable"); n != 2 {
		t.Fatalf("Writable called %d times, want a fresh check after rescan", n)
	}
}

func TestRefreshSavesCache(t *testing.T) {
	f := fakeWithAll()
	f.Err["ListMentions"] = errors.New("boom")
	r := refresher(t, f)
	res := r.Refresh(context.Background())
	cached, ok := r.Cached()
	if !ok || len(cached.Items) != len(res.Items) || !cached.At.Equal(t0) {
		t.Fatalf("cached ok %v items %d at %v", ok, len(cached.Items), cached.At)
	}
	if len(cached.Problems) != 1 || cached.Problems[0].Err == nil || cached.Problems[0].Err.Error() != "boom" {
		t.Fatalf("cached problems = %+v", cached.Problems)
	}
}

func TestCachedEmpty(t *testing.T) {
	if _, ok := refresher(t, forgetest.New()).Cached(); ok {
		t.Fatal("want no cache")
	}
}
