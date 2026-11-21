package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/raven-clown/bridge/forge"
)

var repoA = forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}

func TestListMyPRsPaginates(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		q, _ := r.Variables["q"].(string)
		if !strings.Contains(q, "author:@me") || !strings.Contains(q, "is:pr") {
			t.Errorf("q = %q", q)
		}
		var nodes []any
		if r.Variables["after"] == nil {
			for i := range 100 {
				nodes = append(nodes, issueNode(i+1))
			}
			return ok(searchPage(nodes, 150, true, "c1"))
		}
		if r.Variables["after"] != "c1" {
			t.Errorf("after = %v", r.Variables["after"])
		}
		for i := range 50 {
			nodes = append(nodes, issueNode(i+101))
		}
		return ok(searchPage(nodes, 150, false, "c2"))
	}
	got, err := f.client().ListMyPRs(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 150 || got.Truncated || len(f.requests()) != 2 {
		t.Fatalf("items %d truncated %v requests %d", len(got.Items), got.Truncated, len(f.requests()))
	}
}

func TestListSinceAddsUpdatedFilter(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) { return ok(searchPage(nil, 0, false, "")) }
	since := time.Date(2026, 11, 20, 1, 2, 3, 0, time.UTC)
	if _, err := f.client().ListMyPRs(context.Background(), &since); err != nil {
		t.Fatal(err)
	}
	if q := f.requests()[0].Variables["q"].(string); !strings.Contains(q, "updated:>=2026-11-20T01:02:03Z") {
		t.Fatalf("q = %q", q)
	}
}

func TestListTruncatedAt1000(t *testing.T) {
	f := newFakeGH(t)
	page := 0
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		page++
		var nodes []any
		for i := range 100 {
			nodes = append(nodes, issueNode(page*100+i))
		}
		return ok(searchPage(nodes, 1200, true, fmt.Sprintf("c%d", page)))
	}
	got, err := f.client().ListReviewRequests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1000 || !got.Truncated || page != 10 {
		t.Fatalf("items %d truncated %v pages %d", len(got.Items), got.Truncated, page)
	}
}

func TestIncomingBatchesRepos(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		q := r.Variables["q"].(string)
		if !strings.Contains(q, "-author:@me") {
			t.Errorf("q = %q", q)
		}
		return ok(searchPage([]any{issueNode(len(f.requests()))}, 1, false, ""))
	}
	var repos []forge.RepoID
	for i := range 45 {
		repos = append(repos, forge.RepoID{Host: "github.com", Owner: "o", Name: fmt.Sprintf("r%d", i)})
	}
	got, err := f.client().ListIncoming(context.Background(), repos, nil)
	if err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 3 || len(got.Items) != 3 {
		t.Fatalf("requests %d items %d", len(reqs), len(got.Items))
	}
	seen := map[string]bool{}
	for _, r := range reqs {
		q := r.Variables["q"].(string)
		n := strings.Count(q, "repo:")
		if n > 20 {
			t.Errorf("%d repos in one query", n)
		}
		for _, f := range strings.Fields(q) {
			if strings.HasPrefix(f, "repo:") {
				seen[f] = true
			}
		}
	}
	if len(seen) != 45 {
		t.Fatalf("covered %d repos", len(seen))
	}
}

func TestNodeMappingPR(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(gqlReq) (int, http.Header, any) {
		return ok(searchPage([]any{fixture(t, "pr_node.json")}, 1, false, ""))
	}
	got, err := f.client().ListMyPRs(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	it := got.Items[0]
	if it.ID != (forge.ItemID{Repo: repoA, Number: 42}) || !it.IsPR || it.Title != "Add the thing" || it.Author.Login != "alice" {
		t.Fatalf("basic fields: %+v", it)
	}
	if it.Head != "feat/thing" || len(it.Labels) != 1 || it.Labels[0].Name != "enhancement" {
		t.Fatalf("head/labels: %+v", it)
	}
	if it.Milestone == nil || it.Milestone.Number != 3 || it.Milestone.Due == nil || !it.Milestone.Open {
		t.Fatalf("milestone: %+v", it.Milestone)
	}
	if len(it.Assignees) != 1 || it.Assignees[0].Login != "me" {
		t.Fatalf("assignees: %+v", it.Assignees)
	}
	if it.Checks == nil || it.Checks.State != forge.ChecksFailing || it.Checks.Failed != 1 || it.Checks.Total != 3 {
		t.Fatalf("checks: %+v", it.Checks)
	}
	if it.Review == nil || *it.Review != forge.ReviewApproved || it.Mergeable == nil || *it.Mergeable != forge.MergeableClean {
		t.Fatalf("review/mergeable: %v %v", it.Review, it.Mergeable)
	}
	if it.LastCommentBy != "bob" || it.LastCommentAt.IsZero() {
		t.Fatalf("last comment: %q %v", it.LastCommentBy, it.LastCommentAt)
	}
}

func TestChecksAwaitingApprovalOnFork(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(gqlReq) (int, http.Header, any) {
		return ok(searchPage([]any{fixture(t, "fork_pr_node.json")}, 1, false, ""))
	}
	got, err := f.client().ListIncoming(context.Background(), []forge.RepoID{repoA}, nil)
	if err != nil {
		t.Fatal(err)
	}
	it := got.Items[0]
	if it.Checks == nil || it.Checks.State != forge.ChecksAwaitingApproval {
		t.Fatalf("checks: %+v", it.Checks)
	}
	if it.Mergeable == nil || *it.Mergeable != forge.MergeableBlocked || it.Review == nil || *it.Review != forge.ReviewNone {
		t.Fatalf("mergeable %v review %v", it.Mergeable, it.Review)
	}
}

func TestNodeMappingIssueBot(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(gqlReq) (int, http.Header, any) {
		return ok(searchPage([]any{fixture(t, "issue_node.json")}, 1, false, ""))
	}
	got, err := f.client().ListIncoming(context.Background(), []forge.RepoID{repoA}, nil)
	if err != nil {
		t.Fatal(err)
	}
	it := got.Items[0]
	if it.IsPR || it.Checks != nil || !it.Author.Bot || it.Author.Login != "dependabot[bot]" || it.LastCommentBy != "me" {
		t.Fatalf("issue: %+v", it)
	}
}

func TestListMentionsUsesLatestOtherComment(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		if strings.Contains(r.Query, "viewer") && !strings.Contains(r.Query, "search(") {
			return ok(map[string]any{"viewer": map[string]any{"login": "me"}})
		}
		if q := r.Variables["q"].(string); !strings.Contains(q, "mentions:@me") {
			t.Errorf("q = %q", q)
		}
		return ok(searchPage([]any{fixture(t, "issue_node.json")}, 1, false, ""))
	}
	got, err := f.client().ListMentions(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := got.Items[0]
	if m.Comment != "IC_1" || m.Author.Login != "carol" || m.Item.Number != 5 || m.Title != "Crash on start" || m.Excerpt != "@me can you look" {
		t.Fatalf("mention: %+v", m)
	}
}

func TestListBranches(t *testing.T) {
	f := newFakeGH(t)
	recent := "2026-11-20T00:00:00Z"
	old := "2026-10-01T00:00:00Z"
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		if r.Variables["owner"] != "o" || r.Variables["name"] != "a" {
			t.Errorf("vars = %v", r.Variables)
		}
		ref := func(name, date string, prs int) map[string]any {
			return map[string]any{
				"name":                   name,
				"target":                 map[string]any{"oid": "abc" + name, "committedDate": date},
				"associatedPullRequests": map[string]any{"totalCount": prs},
			}
		}
		return ok(map[string]any{"repository": map[string]any{
			"defaultBranchRef": map[string]any{"name": "main"},
			"refs": map[string]any{"nodes": []any{
				ref("main", recent, 0), ref("feat/new", recent, 0), ref("feat/pr", recent, 1), ref("stale", old, 0),
			}},
		}})
	}
	got, err := f.client().ListBranches(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range got {
		names = append(names, fmt.Sprintf("%s:%v", b.Name, b.HasPR))
	}
	if strings.Join(names, ",") != "feat/new:false,feat/pr:true" {
		t.Fatalf("branches = %v", names)
	}
}

func TestPRChecks(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		if r.Variables["number"] != float64(42) {
			t.Errorf("vars = %v", r.Variables)
		}
		return ok(map[string]any{"repository": map[string]any{"pullRequest": fixture(t, "pr_node.json")}})
	}
	got, err := f.client().PRChecks(context.Background(), forge.ItemID{Repo: repoA, Number: 42})
	if err != nil || got.State != forge.ChecksFailing {
		t.Fatalf("got %+v err %v", got, err)
	}
}
