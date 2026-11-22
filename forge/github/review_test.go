package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/raven-clown/bridge/forge"
)

func pullJSON() map[string]any {
	return map[string]any{
		"number": 42,
		"head":   map[string]any{"sha": "headsha", "ref": "feat/thing"},
		"base":   map[string]any{"ref": "main"},
	}
}

func filesServer(t *testing.T, page func(n int) []any) (*fakeGH, *int) {
	t.Helper()
	f := newFakeGH(t)
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/a/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(pullJSON())
	})
	mux.HandleFunc("GET /repos/o/a/pulls/42/files", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("per_page = %q", r.URL.Query().Get("per_page"))
		}
		n, _ := strconv.Atoi(r.URL.Query().Get("page"))
		_ = json.NewEncoder(w).Encode(page(n))
	})
	f.REST = mux
	return f, &calls
}

func files(n int, start int) []any {
	out := make([]any, n)
	for i := range n {
		out[i] = map[string]any{"filename": fmt.Sprintf("f%d.go", start+i), "status": "modified", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@\n-a\n+b"}
	}
	return out
}

func TestPRDiffPagesFiles(t *testing.T) {
	f, calls := filesServer(t, func(n int) []any {
		if n == 1 {
			return files(100, 0)
		}
		return files(20, 100)
	})
	d, err := f.client().PRDiff(context.Background(), pr42)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 120 || d.Truncated || d.HeadSHA != "headsha" || d.HeadRef != "feat/thing" || d.BaseRef != "main" || *calls != 2 {
		t.Fatalf("files %d truncated %v head %q calls %d", len(d.Files), d.Truncated, d.HeadSHA, *calls)
	}
}

func TestPRDiffTruncatesAt30Pages(t *testing.T) {
	f, calls := filesServer(t, func(n int) []any { return files(100, n*100) })
	d, err := f.client().PRDiff(context.Background(), pr42)
	if err != nil || !d.Truncated || len(d.Files) != 3000 || *calls != 30 {
		t.Fatalf("files %d truncated %v calls %d err %v", len(d.Files), d.Truncated, *calls, err)
	}
}

func TestPRDiffMissingPatch(t *testing.T) {
	f, _ := filesServer(t, func(int) []any {
		return []any{
			map[string]any{"filename": "big.go", "status": "modified", "additions": 5, "deletions": 0},
			map[string]any{"filename": "img.png", "status": "added", "additions": 0, "deletions": 0},
			map[string]any{"filename": "empty.txt", "status": "added", "additions": 0, "deletions": 0, "patch": ""},
			map[string]any{"filename": "new.go", "previous_filename": "old.go", "status": "renamed", "patch": "@@ -1 +1 @@\n x"},
			map[string]any{"filename": "c.go", "status": "changed", "patch": "@@ -1 +1 @@\n x"},
		}
	})
	d, err := f.client().PRDiff(context.Background(), pr42)
	if err != nil {
		t.Fatal(err)
	}
	got := d.Files
	if !got[0].PatchMissing || got[0].Additions != 5 || !got[1].PatchMissing || got[2].PatchMissing {
		t.Fatalf("missing flags: %+v", got[:3])
	}
	if got[3].PreviousPath != "old.go" || got[3].Status != forge.FileRenamed || got[4].Status != forge.FileModified {
		t.Fatalf("rename/status: %+v %+v", got[3], got[4])
	}
}

func TestSubmitReviewBody(t *testing.T) {
	f := newFakeGH(t)
	var body map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/o/a/pulls/42/reviews", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	f.REST = mux
	rv := forge.Review{CommitSHA: "headsha", Event: forge.EventRequestChanges, Body: "please fix", Comments: []forge.ReviewComment{
		{Path: "a.go", Line: 10, Side: forge.SideRight, Body: "one line"},
		{Path: "a.go", Line: 14, Side: forge.SideRight, StartLine: 12, StartSide: forge.SideRight, Body: "a range"},
	}}
	if err := f.client().SubmitReview(context.Background(), pr42, rv); err != nil {
		t.Fatal(err)
	}
	if body["commit_id"] != "headsha" || body["event"] != "REQUEST_CHANGES" || body["body"] != "please fix" {
		t.Fatalf("body = %v", body)
	}
	cs := body["comments"].([]any)
	single, ranged := cs[0].(map[string]any), cs[1].(map[string]any)
	if _, has := single["start_line"]; has || single["line"] != float64(10) || single["side"] != "RIGHT" {
		t.Fatalf("single = %v", single)
	}
	if ranged["start_line"] != float64(12) || ranged["start_side"] != "RIGHT" || ranged["line"] != float64(14) {
		t.Fatalf("range = %v", ranged)
	}
}

func TestSubmitReviewStale422(t *testing.T) {
	f := newFakeGH(t)
	f.REST = restMux(t, map[string]func(map[string]any) (int, any){
		"POST /repos/o/a/pulls/42/reviews": func(map[string]any) (int, any) {
			return 422, map[string]any{"message": "Unprocessable Entity: pull_request_review_thread.line must be part of the diff"}
		},
	})
	err := f.client().SubmitReview(context.Background(), pr42, forge.Review{CommitSHA: "old", Event: forge.EventApprove})
	if !errors.Is(err, forge.ErrStaleHead) {
		t.Fatalf("got %v", err)
	}
}
