package forgetest

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/raven-clown/bridge/forge"
)

var repo = forge.RepoID{Host: "github.com", Owner: "o", Name: "n"}

func TestFakeSetLabelsMutates(t *testing.T) {
	f := New()
	id := forge.ItemID{Repo: repo, Number: 1}
	f.Incoming = []forge.Item{{ID: id, Title: "t"}}
	f.Threads[id] = forge.Thread{Item: f.Incoming[0]}

	if err := f.SetLabels(context.Background(), id, []string{"bug"}, nil); err != nil {
		t.Fatal(err)
	}
	th, err := f.Thread(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Item.Labels) != 1 || th.Item.Labels[0].Name != "bug" {
		t.Fatalf("thread labels = %+v", th.Item.Labels)
	}
	got, _ := f.ListIncoming(context.Background(), []forge.RepoID{repo}, nil)
	if len(got.Items[0].Labels) != 1 {
		t.Fatalf("incoming labels = %+v", got.Items[0].Labels)
	}
}

func TestFakeInjectedError(t *testing.T) {
	f := New()
	f.Err["ListMyPRs"] = io.ErrUnexpectedEOF
	_, err := f.ListMyPRs(context.Background(), nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v", err)
	}
	if len(f.CallLog()) != 1 || f.CallLog()[0] != "ListMyPRs" {
		t.Fatalf("calls = %v", f.CallLog())
	}
}

func TestFakeCommentAppends(t *testing.T) {
	f := New()
	id := forge.ItemID{Repo: repo, Number: 2}
	f.Threads[id] = forge.Thread{Item: forge.Item{ID: id}}
	cid, err := f.Comment(context.Background(), id, "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, _ := f.Thread(context.Background(), id)
	if len(th.Comments) != 1 || th.Comments[0].ID != cid || th.Comments[0].Author.Login != f.Me {
		t.Fatalf("comments = %+v", th.Comments)
	}
}

func TestFakeSubmitReviewRecords(t *testing.T) {
	f := New()
	pr := forge.ItemID{Repo: repo, Number: 3}
	f.Diffs[pr] = forge.Diff{PR: pr, HeadSHA: "abc"}
	rv := forge.Review{CommitSHA: "abc", Event: forge.EventApprove}
	if err := f.SubmitReview(context.Background(), pr, rv); err != nil {
		t.Fatal(err)
	}
	if len(f.Reviews[pr]) != 1 || f.Reviews[pr][0].Event != forge.EventApprove {
		t.Fatalf("reviews = %+v", f.Reviews[pr])
	}
	d, err := f.PRDiff(context.Background(), pr)
	if err != nil || d.HeadSHA != "abc" {
		t.Fatalf("diff %+v err %v", d, err)
	}
}

func TestFakeSubmitReviewStaleHead(t *testing.T) {
	f := New()
	pr := forge.ItemID{Repo: repo, Number: 3}
	f.Diffs[pr] = forge.Diff{PR: pr, HeadSHA: "new"}
	err := f.SubmitReview(context.Background(), pr, forge.Review{CommitSHA: "old", Event: forge.EventComment, Body: "x"})
	if !errors.Is(err, forge.ErrStaleHead) || len(f.Reviews[pr]) != 0 {
		t.Fatalf("err %v reviews %+v", err, f.Reviews[pr])
	}
}

func TestFakeImplementsForge(t *testing.T) {
	var _ forge.Forge = New()
}
