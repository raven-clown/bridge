package inbox

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
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
