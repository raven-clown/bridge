package inbox

import (
	"testing"
	"time"

	"github.com/raven-clown/bridge/forge"
)

var (
	repoA = forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}
	t0    = time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
)

func id(n int) forge.ItemID { return forge.ItemID{Repo: repoA, Number: n} }

func noSeen(forge.ItemID) (*time.Time, *time.Time) { return nil, nil }

func noMute(forge.InboxKind, forge.RepoID, forge.User) bool { return false }

func input() Input {
	return Input{Me: "me", Seen: noSeen, Muted: noMute, Branches: map[forge.RepoID][]forge.Branch{}, Local: map[forge.RepoID]string{}}
}

func kinds(items []Item) map[forge.ItemID]forge.InboxKind {
	out := map[forge.ItemID]forge.InboxKind{}
	for _, it := range items {
		out[it.ID] = it.Kind
	}
	return out
}

func TestBuildKinds(t *testing.T) {
	in := input()
	in.MyPRs = []forge.Item{{ID: id(1), IsPR: true, Author: forge.User{Login: "me"}}}
	in.Incoming = []forge.Item{{ID: id(2), IsPR: true}, {ID: id(3)}}
	in.ReviewRequests = []forge.Item{{ID: id(4), IsPR: true}}
	in.Local[repoA] = "/src/a"
	got := Build(in)
	k := kinds(got)
	if k[id(1)] != forge.OwnPR || k[id(2)] != forge.IncomingPR || k[id(3)] != forge.IncomingIssue || k[id(4)] != forge.ReviewRequest {
		t.Fatalf("kinds = %v", k)
	}
	for _, it := range got {
		if it.Local != "/src/a" {
			t.Errorf("%v: local %q", it.ID, it.Local)
		}
	}
}

func TestBuildMergesOwnPRAndMention(t *testing.T) {
	in := input()
	in.MyPRs = []forge.Item{{ID: id(1), IsPR: true, Title: "mine"}}
	in.Mentions = []forge.CommentRef{{Item: id(1), Comment: "C1", Author: forge.User{Login: "x"}, At: t0}}
	got := Build(in)
	if len(got) != 1 || got[0].Kind != forge.OwnPR || got[0].Unanswered != 1 || got[0].Urgency != UrgencyUnanswered {
		t.Fatalf("got %+v", got)
	}
}

func TestBuildMentionOnly(t *testing.T) {
	in := input()
	in.Mentions = []forge.CommentRef{{Item: id(9), Title: "elsewhere", Comment: "C9", Author: forge.User{Login: "x"}, At: t0}}
	got := Build(in)
	if len(got) != 1 || got[0].Kind != forge.Mention || got[0].Title != "elsewhere" || got[0].Unanswered != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestBuildAnsweredMentionDropped(t *testing.T) {
	in := input()
	answered := t0.Add(time.Hour)
	in.Seen = func(forge.ItemID) (*time.Time, *time.Time) { return nil, &answered }
	in.Mentions = []forge.CommentRef{{Item: id(9), Comment: "C9", Author: forge.User{Login: "x"}, At: t0}}
	if got := Build(in); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestBuildReviewRequestBeatsIncoming(t *testing.T) {
	in := input()
	in.Incoming = []forge.Item{{ID: id(4), IsPR: true}}
	in.ReviewRequests = []forge.Item{{ID: id(4), IsPR: true}}
	got := Build(in)
	if len(got) != 1 || got[0].Kind != forge.ReviewRequest {
		t.Fatalf("got %+v", got)
	}
}

func TestBuildUnansweredUsesAnsweredAt(t *testing.T) {
	in := input()
	answered := t0
	in.Seen = func(forge.ItemID) (*time.Time, *time.Time) { return nil, &answered }
	in.Incoming = []forge.Item{
		{ID: id(1), LastCommentBy: "x", LastCommentAt: t0.Add(-time.Minute)},
		{ID: id(2), LastCommentBy: "x", LastCommentAt: t0.Add(time.Minute)},
		{ID: id(3), LastCommentBy: "me", LastCommentAt: t0.Add(time.Minute)},
	}
	got := map[forge.ItemID]int{}
	for _, it := range Build(in) {
		got[it.ID] = it.Unanswered
	}
	if got[id(1)] != 0 || got[id(2)] != 1 || got[id(3)] != 0 {
		t.Fatalf("unanswered = %v", got)
	}
}

func TestBuildDropsMuted(t *testing.T) {
	in := input()
	in.Incoming = []forge.Item{{ID: id(1), Author: forge.User{Login: "dependabot[bot]"}}, {ID: id(2), Author: forge.User{Login: "x"}}}
	in.Muted = func(_ forge.InboxKind, _ forge.RepoID, u forge.User) bool { return u.Login == "dependabot[bot]" }
	got := Build(in)
	if len(got) != 1 || got[0].ID != id(2) {
		t.Fatalf("got %+v", got)
	}
}

func TestBuildSortsByUrgencyThenAge(t *testing.T) {
	in := input()
	in.Incoming = []forge.Item{
		{ID: id(1), Labels: []forge.Label{{Name: "x"}}, UpdatedAt: t0},
		{ID: id(2), UpdatedAt: t0.Add(2 * time.Hour)},
		{ID: id(3), UpdatedAt: t0.Add(time.Hour)},
	}
	in.ReviewRequests = []forge.Item{{ID: id(4), IsPR: true, UpdatedAt: t0.Add(3 * time.Hour)}}
	got := Build(in)
	var order []int
	for _, it := range got {
		order = append(order, it.ID.Number)
	}
	want := []int{4, 3, 2, 1}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestBuildPushedBranchSkipsWithPR(t *testing.T) {
	in := input()
	in.Branches[repoA] = []forge.Branch{
		{Name: "feat/x", PushedAt: t0},
		{Name: "feat/y", PushedAt: t0, HasPR: true},
	}
	got := Build(in)
	if len(got) != 1 || got[0].Kind != forge.PushedBranch || got[0].Branch != "feat/x" || got[0].Repo != repoA {
		t.Fatalf("got %+v", got)
	}
}

func TestBuildCarriesSeenAt(t *testing.T) {
	in := input()
	seen := t0
	in.Seen = func(forge.ItemID) (*time.Time, *time.Time) { return &seen, nil }
	in.Incoming = []forge.Item{{ID: id(1)}}
	got := Build(in)
	if got[0].SeenAt == nil || !got[0].SeenAt.Equal(t0) {
		t.Fatalf("seen = %v", got[0].SeenAt)
	}
}
