package review

import (
	"path/filepath"
	"testing"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/store"
)

var pr = forge.PRID{Repo: forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}, Number: 42}

func TestDraftStale(t *testing.T) {
	d := Draft{PR: pr, HeadSHA: "a"}
	if d.Stale("a") || !d.Stale("b") {
		t.Fatal("stale check wrong")
	}
}

func TestDraftViewedToggle(t *testing.T) {
	var d Draft
	d.ToggleViewed("a.go")
	if !d.IsViewed("a.go") || d.IsViewed("b.go") {
		t.Fatal("not viewed after toggle")
	}
	d.ToggleViewed("a.go")
	if d.IsViewed("a.go") {
		t.Fatal("still viewed after second toggle")
	}
}

func TestDraftRemoveKeepsOrder(t *testing.T) {
	var d Draft
	for _, b := range []string{"one", "two", "three"} {
		d.Add(DraftComment{Path: "a.go", Line: 1, Side: forge.SideRight, Body: b})
	}
	d.Remove(1)
	if len(d.Comments) != 2 || d.Comments[0].Body != "one" || d.Comments[1].Body != "three" {
		t.Fatalf("comments = %+v", d.Comments)
	}
	d.Remove(9)
	if len(d.Comments) != 2 {
		t.Fatal("out of range remove changed the draft")
	}
}

func TestDraftSaveLoad(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LoadDraft(s, pr); ok || err != nil {
		t.Fatalf("empty: ok %v err %v", ok, err)
	}
	d := Draft{PR: pr, HeadSHA: "abc", Summary: "s", Viewed: []string{"a.go"}}
	d.Add(DraftComment{Path: "a.go", Line: 3, Side: forge.SideRight, StartLine: 1, StartSide: forge.SideRight, Body: "x"})
	if err := SaveDraft(s, d); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadDraft(s, pr)
	if err != nil || !ok || got.HeadSHA != "abc" || len(got.Comments) != 1 || got.Comments[0].StartLine != 1 || !got.IsViewed("a.go") {
		t.Fatalf("got %+v ok %v err %v", got, ok, err)
	}
}
