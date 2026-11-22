package threadview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/forgetest"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/tuitest"
	"github.com/raven-clown/bridge/store"
)

func TestMain(m *testing.M) {
	tuitest.NoColor()
	os.Exit(m.Run())
}

var (
	now  = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	repo = forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}
	id   = forge.ItemID{Repo: repo, Number: 42}
)

func setup(t *testing.T) (Model, *forgetest.Fake, *store.Store) {
	t.Helper()
	f := forgetest.New()
	f.Threads[id] = forge.Thread{
		Item: forge.Item{ID: id, IsPR: true, Title: "Add the thing", Author: forge.User{Login: "alice"},
			Labels: []forge.Label{{Name: "enhancement"}}, URL: "https://github.com/o/a/pull/42"},
		Body: "This adds **the thing**.",
		Comments: []forge.Comment{
			{ID: "IC_1", Author: forge.User{Login: "bob"}, At: now.Add(-3 * time.Hour), Body: "Nice."},
			{ID: "IC_2", Author: forge.User{Login: "carol"}, At: now.Add(-2 * time.Hour), Body: "Can you add a test?"},
		},
		ReviewThreads: []forge.ReviewThread{{ID: "PRRT_1", Path: "main.go", Line: 12, Comments: []forge.Comment{
			{ID: "PRRC_1", Author: forge.User{Login: "carol"}, At: now.Add(-time.Hour), Body: "typo here"},
		}}},
		Events: []forge.Event{{At: now.Add(-4 * time.Hour), Actor: forge.User{Login: "me"}, Kind: "labeled", Detail: "enhancement"}},
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(id, Deps{Forge: f, Store: s, Now: func() time.Time { return now }}, 80, 40)
	for _, msg := range tuitest.Run(m.Init()) {
		m, _ = m.Update(msg)
	}
	return m, f, s
}

func press(m Model, keys ...string) (Model, []tea.Msg) {
	var out []tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(tuitest.Key(k))
		for _, msg := range tuitest.Run(cmd) {
			var next tea.Cmd
			m, next = m.Update(msg)
			out = append(out, msg)
			out = append(out, tuitest.Run(next)...)
		}
	}
	return m, out
}

func typeText(m Model, s string) Model {
	for _, k := range tuitest.Type(s) {
		m, _ = m.Update(k)
	}
	return m
}

func TestThreadGolden(t *testing.T) {
	m, _, _ := setup(t)
	tuitest.Golden(t, "thread_basic", m.View())
}

func TestThreadShowsHiddenCountAndReview(t *testing.T) {
	_, f, s := setup(t)
	th := f.Threads[id]
	th.HiddenComments = 40
	th.Comments = append(th.Comments, forge.Comment{ID: "PRR_1", Author: forge.User{Login: "dave"}, At: now.Add(-30 * time.Minute), Body: "Ship it", Review: "approved"})
	f.Threads[id] = th
	m := New(id, Deps{Forge: f, Store: s, Now: func() time.Time { return now }}, 80, 60)
	for _, msg := range tuitest.Run(m.Init()) {
		m, _ = m.Update(msg)
	}
	view := m.View()
	if !strings.Contains(view, "40 earlier comments are not shown") || !strings.Contains(view, "dave · approved") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestThreadLoadError(t *testing.T) {
	f := forgetest.New()
	s, _ := store.Open(filepath.Join(t.TempDir(), "state"))
	m := New(id, Deps{Forge: f, Store: s, Now: func() time.Time { return now }}, 80, 40)
	for _, msg := range tuitest.Run(m.Init()) {
		m, _ = m.Update(msg)
	}
	if !strings.Contains(m.View(), "not found") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestReplyFlow(t *testing.T) {
	m, f, s := setup(t)
	m, _ = press(m, "j", "j", "r")
	if !m.Composing() || !strings.Contains(m.View(), "Reply to carol") {
		t.Fatalf("not composing a reply to carol:\n%s", m.View())
	}
	m = typeText(m, "thanks")
	m, _ = press(m, "ctrl+s")
	th, _ := f.Thread(t.Context(), id)
	last := th.Comments[len(th.Comments)-1]
	if last.Body != "thanks" || last.ReplyTo != "IC_2" {
		t.Fatalf("last comment = %+v", last)
	}
	if _, answered := s.Seen(id); answered == nil {
		t.Fatal("not marked answered")
	}
	if m.Composing() {
		t.Fatal("compose still open after sending")
	}
}

func TestReplyToReviewCommentUsesThreadID(t *testing.T) {
	m, f, _ := setup(t)
	m, _ = press(m, "G", "r")
	m = typeText(m, "fixed")
	_, _ = press(m, "ctrl+s")
	th, _ := f.Thread(t.Context(), id)
	if last := th.Comments[len(th.Comments)-1]; last.ReplyTo != "PRRT_1" {
		t.Fatalf("reply to = %q", last.ReplyTo)
	}
}

func TestNewCommentHasNoReplyTo(t *testing.T) {
	m, f, _ := setup(t)
	m, _ = press(m, "j", "c")
	m = typeText(m, "top level")
	_, _ = press(m, "ctrl+s")
	th, _ := f.Thread(t.Context(), id)
	if last := th.Comments[len(th.Comments)-1]; last.ReplyTo != "" || last.Body != "top level" {
		t.Fatalf("last = %+v", last)
	}
}

func TestReplyErrorKeepsText(t *testing.T) {
	m, f, s := setup(t)
	f.Err["Comment"] = forge.ErrNotFound
	m, _ = press(m, "j", "r")
	m = typeText(m, "thanks")
	m, _ = press(m, "ctrl+s")
	view := m.View()
	if !m.Composing() || !strings.Contains(view, "thanks") || !strings.Contains(view, "not found") {
		t.Fatalf("view:\n%s", view)
	}
	if _, body, ok, _ := s.LoadReplyDraft(id); !ok || body != "thanks" {
		t.Fatalf("draft %q ok %v", body, ok)
	}
}

func TestTypingSavesDraft(t *testing.T) {
	m, _, s := setup(t)
	m, _ = press(m, "j", "r")
	_ = typeText(m, "typed only")
	if _, body, ok, _ := s.LoadReplyDraft(id); !ok || body != "typed only" {
		t.Fatalf("draft %q ok %v", body, ok)
	}
}

func TestEscKeepsDraft(t *testing.T) {
	m, _, s := setup(t)
	m, _ = press(m, "j", "r")
	m = typeText(m, "half done")
	m, _ = press(m, "esc")
	if m.Composing() {
		t.Fatal("still composing")
	}
	if _, body, ok, _ := s.LoadReplyDraft(id); !ok || body != "half done" {
		t.Fatalf("draft %q ok %v", body, ok)
	}
	m, _ = press(m, "r")
	if !strings.Contains(m.View(), "half done") {
		t.Fatalf("draft not loaded:\n%s", m.View())
	}
}

func TestLateLoadForAnotherItemIsIgnored(t *testing.T) {
	f := forgetest.New()
	s, _ := store.Open(filepath.Join(t.TempDir(), "state"))
	other := forge.ItemID{Repo: repo, Number: 7}
	m := New(id, Deps{Forge: f, Store: s, Now: func() time.Time { return now }}, 80, 40)
	m, _ = m.Update(loadedMsg{id: other, th: forge.Thread{Item: forge.Item{ID: other, Title: "Wrong thread"}}})
	if strings.Contains(m.View(), "Wrong thread") {
		t.Fatalf("another item's thread was shown:\n%s", m.View())
	}
	m, _ = m.Update(sentMsg{id: other})
	if m.Composing() {
		t.Fatal("unexpected compose state")
	}
}

func TestThreadKeys(t *testing.T) {
	m, _, _ := setup(t)
	_, out := press(m, "t")
	if tr, ok := out[0].(msgs.OpenTriage); !ok || len(tr.IDs) != 1 || tr.IDs[0] != id {
		t.Fatalf("t: %+v", out)
	}
	_, out = press(m, "q")
	if out[0] != (msgs.Back{}) {
		t.Fatalf("q: %+v", out)
	}
	_, out = press(m, "e")
	if s, ok := out[0].(msgs.Status); !ok || !strings.Contains(s.Text, "v0.2") {
		t.Fatalf("e: %+v", out)
	}
	_, out = press(m, "o")
	if out[0] != (msgs.OpenURL{URL: "https://github.com/o/a/pull/42"}) {
		t.Fatalf("o: %+v", out)
	}
}

func TestThreadEscapesHostileText(t *testing.T) {
	_, f, s := setup(t)
	th := f.Threads[id]
	th.Item.Title = "t\x1b]0;pwned\x07"
	th.Body = "hello &#27;]0;body&#7; and &#x1b;[2J world"
	th.Comments[0].Body = "c\x1b[2J and &#x1B;]52;c;ZXZpbA==&#x07;"
	th.ReviewThreads[0].Comments[0].Body = "r &#27;]8;;https://evil&#27;\\link"
	f.Threads[id] = th
	m := New(id, Deps{Forge: f, Store: s, Now: func() time.Time { return now }}, 80, 40)
	for _, msg := range tuitest.Run(m.Init()) {
		m, _ = m.Update(msg)
	}
	view := m.View()
	for _, bad := range []string{"\x07", "\x1b]", "\x1b[2J"} {
		if strings.Contains(view, bad) {
			t.Fatalf("raw %q in view: %q", bad, view)
		}
	}
}
