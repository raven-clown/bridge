package store

import "testing"

func TestReviewDraftRoundTrip(t *testing.T) {
	s, _ := open(t)
	type draft struct{ Summary string }
	if err := s.SaveReviewDraft(item, draft{Summary: "looks good"}); err != nil {
		t.Fatal(err)
	}
	var got draft
	ok, err := s.LoadReviewDraft(item, &got)
	if err != nil || !ok || got.Summary != "looks good" {
		t.Fatalf("got %+v ok %v err %v", got, ok, err)
	}
	if err := s.DeleteReviewDraft(item); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.LoadReviewDraft(item, &got); ok || err != nil {
		t.Fatalf("after delete ok %v err %v", ok, err)
	}
	if err := s.DeleteReviewDraft(item); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestReviewAndReplyDraftsAreSeparate(t *testing.T) {
	s, _ := open(t)
	if err := s.SaveReplyDraft(item, "", "reply text"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveReviewDraft(item, map[string]string{"summary": "review text"}); err != nil {
		t.Fatal(err)
	}
	if _, body, ok, _ := s.LoadReplyDraft(item); !ok || body != "reply text" {
		t.Fatalf("reply draft lost: %q", body)
	}
}
