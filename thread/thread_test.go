package thread

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/forgetest"
	"github.com/raven-clown/bridge/store"
)

var id = forge.ItemID{Repo: forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}, Number: 1}

func setup(t *testing.T) (*forgetest.Fake, *store.Store) {
	t.Helper()
	f := forgetest.New()
	f.Threads[id] = forge.Thread{Item: forge.Item{ID: id}, Body: "b"}
	s, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	return f, s
}

func TestLoadMarksSeen(t *testing.T) {
	f, s := setup(t)
	th, err := Load(context.Background(), f, s, id)
	if err != nil || th.Body != "b" {
		t.Fatalf("thread %+v err %v", th, err)
	}
	if seen, _ := s.Seen(id); seen == nil {
		t.Fatal("not marked seen")
	}
}

func TestLoadErrorDoesNotMarkSeen(t *testing.T) {
	f, s := setup(t)
	f.Err["Thread"] = forge.ErrNotFound
	if _, err := Load(context.Background(), f, s, id); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if seen, _ := s.Seen(id); seen != nil {
		t.Fatal("marked seen on error")
	}
}

func TestReplyKeepsDraftOnError(t *testing.T) {
	f, s := setup(t)
	f.Err["Comment"] = forge.ErrNotFound
	if _, err := Reply(context.Background(), f, s, id, "C1", "thanks"); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	to, body, ok, err := s.LoadReplyDraft(id)
	if err != nil || !ok || to != "C1" || body != "thanks" {
		t.Fatalf("draft to %q body %q ok %v err %v", to, body, ok, err)
	}
	if _, answered := s.Seen(id); answered != nil {
		t.Fatal("marked answered on error")
	}
}

func TestReplyClearsDraftAndAnswers(t *testing.T) {
	f, s := setup(t)
	cid, err := Reply(context.Background(), f, s, id, "", "thanks")
	if err != nil || cid == "" {
		t.Fatalf("cid %q err %v", cid, err)
	}
	if _, _, ok, _ := s.LoadReplyDraft(id); ok {
		t.Fatal("draft still there")
	}
	if _, answered := s.Seen(id); answered == nil {
		t.Fatal("not marked answered")
	}
	th, _ := f.Thread(context.Background(), id)
	if len(th.Comments) != 1 || th.Comments[0].ReplyTo != "" {
		t.Fatalf("comments = %+v", th.Comments)
	}
}

func TestReplyPassesReplyTo(t *testing.T) {
	f, s := setup(t)
	if _, err := Reply(context.Background(), f, s, id, "PRRT_1", "fixed"); err != nil {
		t.Fatal(err)
	}
	th, _ := f.Thread(context.Background(), id)
	if th.Comments[0].ReplyTo != "PRRT_1" {
		t.Fatalf("reply to = %q", th.Comments[0].ReplyTo)
	}
}

func TestReplyEmptyBody(t *testing.T) {
	f, s := setup(t)
	if _, err := Reply(context.Background(), f, s, id, "", "  \n "); !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("got %v", err)
	}
	if len(f.CallLog()) != 0 {
		t.Fatalf("calls = %v", f.CallLog())
	}
}
