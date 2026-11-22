package review

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/forgetest"
	"github.com/raven-clown/bridge/store"
)

func submitSetup(t *testing.T, head forge.SHA) (*forgetest.Fake, *store.Store) {
	t.Helper()
	f := forgetest.New()
	f.Diffs[pr] = forge.Diff{PR: pr, HeadSHA: head}
	s, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	return f, s
}

func withComment(d Draft) Draft {
	d.Add(DraftComment{Path: "a.go", Line: 3, Side: forge.SideRight, Body: "nit"})
	return d
}

func TestSubmitApproveEmptyIsFine(t *testing.T) {
	f, s := submitSetup(t, "h")
	if err := Submit(context.Background(), f, s, Draft{PR: pr, HeadSHA: "h"}, forge.EventApprove, false); err != nil {
		t.Fatal(err)
	}
	if len(f.Reviews[pr]) != 1 || f.Reviews[pr][0].Event != forge.EventApprove {
		t.Fatalf("reviews = %+v", f.Reviews[pr])
	}
}

func TestSubmitCommentNeedsSummary(t *testing.T) {
	f, s := submitSetup(t, "h")
	for _, ev := range []forge.ReviewEvent{forge.EventComment, forge.EventRequestChanges} {
		d := withComment(Draft{PR: pr, HeadSHA: "h", Summary: "  "})
		if err := Submit(context.Background(), f, s, d, ev, false); !errors.Is(err, ErrNeedsSummary) {
			t.Fatalf("%s: got %v", ev, err)
		}
	}
	if slices.Contains(f.CallLog(), "SubmitReview") {
		t.Fatal("submitted without a summary")
	}
}

func TestSubmitStaleAsksFirst(t *testing.T) {
	f, s := submitSetup(t, "new")
	d := withComment(Draft{PR: pr, HeadSHA: "old", Summary: "s"})
	if err := SaveDraft(s, d); err != nil {
		t.Fatal(err)
	}
	if err := Submit(context.Background(), f, s, d, forge.EventComment, false); !errors.Is(err, forge.ErrStaleHead) {
		t.Fatalf("got %v", err)
	}
	if slices.Contains(f.CallLog(), "SubmitReview") {
		t.Fatal("submitted against a moved head")
	}
	if _, ok, _ := LoadDraft(s, pr); !ok {
		t.Fatal("draft lost")
	}
}

func TestSubmitStaleForced(t *testing.T) {
	f, s := submitSetup(t, "new")
	d := withComment(Draft{PR: pr, HeadSHA: "old", Summary: "s"})
	if err := Submit(context.Background(), f, s, d, forge.EventComment, true); err != nil {
		t.Fatal(err)
	}
	if got := f.Reviews[pr][0]; got.CommitSHA != "new" || len(got.Comments) != 1 || got.Body != "s" {
		t.Fatalf("review = %+v", got)
	}
}

func TestSubmitErrorKeepsDraft(t *testing.T) {
	f, s := submitSetup(t, "h")
	f.Err["SubmitReview"] = errors.New("boom")
	d := withComment(Draft{PR: pr, HeadSHA: "h", Summary: "s"})
	if err := SaveDraft(s, d); err != nil {
		t.Fatal(err)
	}
	if err := Submit(context.Background(), f, s, d, forge.EventComment, false); err == nil {
		t.Fatal("want error")
	}
	if got, ok, _ := LoadDraft(s, pr); !ok || len(got.Comments) != 1 {
		t.Fatal("draft lost after a failed submit")
	}
}

func TestSubmitDeletesDraftOnSuccess(t *testing.T) {
	f, s := submitSetup(t, "h")
	d := withComment(Draft{PR: pr, HeadSHA: "h", Summary: "s"})
	if err := SaveDraft(s, d); err != nil {
		t.Fatal(err)
	}
	if err := Submit(context.Background(), f, s, d, forge.EventRequestChanges, false); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := LoadDraft(s, pr); ok {
		t.Fatal("draft kept after success")
	}
	if rc := f.Reviews[pr][0].Comments[0]; rc.Path != "a.go" || rc.Line != 3 || rc.Side != forge.SideRight {
		t.Fatalf("comment = %+v", rc)
	}
}
