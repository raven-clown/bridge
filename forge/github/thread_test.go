package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/raven-clown/bridge/forge"
)

var pr42 = forge.ItemID{Repo: repoA, Number: 42}

func scopeError() (int, http.Header, any) {
	return 200, nil, map[string]any{"data": nil, "errors": []any{map[string]any{
		"type":    "INSUFFICIENT_SCOPES",
		"message": "Your token has not been granted the required scopes to execute this query. The 'projectsV2' field requires one of the following scopes: ['read:project']",
	}}}
}

func TestThreadPR(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		if strings.Contains(r.Query, "projectItems") {
			return ok(map[string]any{"repository": map[string]any{"issueOrPullRequest": map[string]any{
				"projectItems": map[string]any{"nodes": []any{map[string]any{
					"id": "PVTI_1", "project": map[string]any{"id": "PVT_1"},
					"fieldValueByName": map[string]any{"name": "In Progress"},
				}}},
			}}})
		}
		return ok(map[string]any{"repository": map[string]any{"issueOrPullRequest": fixture(t, "thread_pr.json")}})
	}
	th, err := f.client().Thread(context.Background(), pr42)
	if err != nil {
		t.Fatal(err)
	}
	if th.Body != "This adds the thing." || th.Item.ID != pr42 || !th.Item.IsPR {
		t.Fatalf("thread = %+v", th)
	}
	if len(th.Comments) != 3 || th.Comments[1].Author.Login != "carol" || th.Comments[1].Body != "Can you add a test?" {
		t.Fatalf("comments = %+v", th.Comments)
	}
	if len(th.ReviewThreads) != 1 || th.ReviewThreads[0].ID != "PRRT_1" || th.ReviewThreads[0].Path != "main.go" || th.ReviewThreads[0].Line != 12 {
		t.Fatalf("review threads = %+v", th.ReviewThreads)
	}
	if len(th.Events) != 2 || th.Events[0].Kind != "labeled" || th.Events[0].Detail != "enhancement" || th.Events[1].Kind != "review_requested" || th.Events[1].Detail != "me" {
		t.Fatalf("events = %+v", th.Events)
	}
	if len(th.Item.Projects) != 1 || th.Item.Projects[0].Column != "In Progress" || th.Item.Projects[0].ItemID != "PVTI_1" {
		t.Fatalf("projects = %+v", th.Item.Projects)
	}
}

func TestThreadIssueHasNoReviewThreads(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		if strings.Contains(r.Query, "projectItems") {
			return ok(map[string]any{"repository": map[string]any{"issueOrPullRequest": map[string]any{"projectItems": map[string]any{"nodes": []any{}}}}})
		}
		n := issueNode(5)
		n["body"] = "it crashes"
		n["allComments"] = map[string]any{"nodes": []any{}}
		n["timelineItems"] = map[string]any{"nodes": []any{map[string]any{"__typename": "ClosedEvent", "createdAt": "2026-11-21T00:00:00Z", "actor": map[string]any{"login": "me"}}}}
		return ok(map[string]any{"repository": map[string]any{"issueOrPullRequest": n}})
	}
	th, err := f.client().Thread(context.Background(), forge.ItemID{Repo: repoA, Number: 5})
	if err != nil {
		t.Fatal(err)
	}
	if th.Item.IsPR || len(th.ReviewThreads) != 0 || th.Body != "it crashes" || len(th.Events) != 1 || th.Events[0].Kind != "closed" {
		t.Fatalf("thread = %+v", th)
	}
}

func TestThreadProjectsForbiddenStillLoads(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		if strings.Contains(r.Query, "projectItems") {
			return scopeError()
		}
		return ok(map[string]any{"repository": map[string]any{"issueOrPullRequest": fixture(t, "thread_pr.json")}})
	}
	th, err := f.client().Thread(context.Background(), pr42)
	if err != nil || len(th.Comments) != 3 || len(th.Item.Projects) != 0 {
		t.Fatalf("thread %+v err %v", th, err)
	}
}

func TestThreadNotFound(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		return ok(map[string]any{"repository": map[string]any{"issueOrPullRequest": nil}})
	}
	if _, err := f.client().Thread(context.Background(), pr42); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func restMux(t *testing.T, routes map[string]func(body map[string]any) (int, any)) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, fn := range routes {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			data, _ := io.ReadAll(r.Body)
			if len(data) > 0 {
				if err := json.Unmarshal(data, &body); err != nil {
					t.Errorf("%s: bad body %s", pattern, data)
				}
			}
			status, out := fn(body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			if out != nil {
				_ = json.NewEncoder(w).Encode(out)
			}
		})
	}
	return mux
}

func TestCommentIssueUsesREST(t *testing.T) {
	f := newFakeGH(t)
	f.REST = restMux(t, map[string]func(map[string]any) (int, any){
		"POST /repos/o/a/issues/42/comments": func(b map[string]any) (int, any) {
			if b["body"] != "thanks" {
				t.Errorf("body = %v", b)
			}
			return 201, map[string]any{"node_id": "IC_99"}
		},
	})
	replyTo := forge.CommentID("IC_2")
	id, err := f.client().Comment(context.Background(), pr42, "thanks", &replyTo)
	if err != nil || id != "IC_99" {
		t.Fatalf("id %q err %v", id, err)
	}
}

func TestCommentReplyUsesReviewThreadMutation(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		if !strings.Contains(r.Query, "addPullRequestReviewThreadReply") || r.Variables["thread"] != "PRRT_1" || r.Variables["body"] != "fixed" {
			t.Errorf("request = %+v", r)
		}
		return ok(map[string]any{"addPullRequestReviewThreadReply": map[string]any{"comment": map[string]any{"id": "PRRC_9"}}})
	}
	replyTo := forge.CommentID("PRRT_1")
	id, err := f.client().Comment(context.Background(), pr42, "fixed", &replyTo)
	if err != nil || id != "PRRC_9" {
		t.Fatalf("id %q err %v", id, err)
	}
}

func TestCommentNotFound(t *testing.T) {
	f := newFakeGH(t)
	f.REST = restMux(t, map[string]func(map[string]any) (int, any){
		"POST /repos/o/a/issues/42/comments": func(map[string]any) (int, any) { return 404, map[string]any{"message": "Not Found"} },
	})
	if _, err := f.client().Comment(context.Background(), pr42, "x", nil); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestCommentLocked(t *testing.T) {
	f := newFakeGH(t)
	f.REST = restMux(t, map[string]func(map[string]any) (int, any){
		"POST /repos/o/a/issues/42/comments": func(map[string]any) (int, any) {
			return 422, map[string]any{"message": "Unable to create comment because issue is locked."}
		},
	})
	if _, err := f.client().Comment(context.Background(), pr42, "x", nil); !errors.Is(err, forge.ErrForbidden) {
		t.Fatalf("got %v", err)
	}
}
