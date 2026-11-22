package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/forgetest"
	"github.com/raven-clown/bridge/inbox"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/tuitest"
	"github.com/raven-clown/bridge/store"
)

func TestMain(m *testing.M) {
	tuitest.NoColor()
	os.Exit(m.Run())
}

var (
	now   = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	repoA = forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}
)

func deps(t *testing.T) (Deps, *forgetest.Fake) {
	t.Helper()
	f := forgetest.New()
	s, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(t.TempDir(), "none.yaml"), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return now }
	r := &inbox.Refresher{Forge: f, Store: s, Config: cfg, Me: "me", Extra: []forge.RepoID{repoA}, Now: clock}
	return Deps{Refresher: r, Forge: f, Store: s, Config: cfg, Me: "me", Now: clock}, f
}

func sized(m Model) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(Model)
}

func TestAppStartsFromCache(t *testing.T) {
	d, _ := deps(t)
	res := inbox.Result{Items: []inbox.Item{{Kind: forge.IncomingIssue, Repo: repoA, ID: forge.ItemID{Repo: repoA, Number: 1}, Title: "From the cache"}}, At: now.Add(-5 * time.Minute)}
	if err := d.Store.SaveCache("inbox", res, res.At); err != nil {
		t.Fatal(err)
	}
	view := sized(newApp(context.Background(), d)).View()
	if !strings.Contains(view, "From the cache") || !strings.Contains(view, "cached") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestAppRateLimitedRefreshKeepsCachedItems(t *testing.T) {
	d, f := deps(t)
	res := inbox.Result{Items: []inbox.Item{{Kind: forge.IncomingIssue, Repo: repoA, ID: forge.ItemID{Repo: repoA, Number: 1}, Title: "Still here"}}, At: now}
	if err := d.Store.SaveCache("inbox", res, now); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"ListMyPRs", "ListIncoming", "ListReviewRequests", "ListMentions"} {
		f.Err[m] = &forge.RateLimitedError{RetryAt: now.Add(10 * time.Minute)}
	}
	m := sized(newApp(context.Background(), d))
	next, _ := m.Update(refreshDoneMsg(d.Refresher.Refresh(context.Background())))
	view := next.View()
	if !strings.Contains(view, "Still here") || !strings.Contains(view, "retry after") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestAppRefreshDoneShowsItems(t *testing.T) {
	d, f := deps(t)
	f.Incoming = []forge.Item{{ID: forge.ItemID{Repo: repoA, Number: 2}, Title: "Fresh one", UpdatedAt: now}}
	m := sized(newApp(context.Background(), d))
	next, _ := m.Update(refreshDoneMsg(d.Refresher.Refresh(context.Background())))
	view := next.View()
	if !strings.Contains(view, "Fresh one") || strings.Contains(view, "cached") || !strings.Contains(view, "refreshed 0m ago") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestAppInitRefreshes(t *testing.T) {
	d, f := deps(t)
	f.Incoming = []forge.Item{{ID: forge.ItemID{Repo: repoA, Number: 3}, Title: "x"}}
	m := newApp(context.Background(), d)
	found := false
	for _, msg := range tuitest.Run(m.refresh()) {
		if _, ok := msg.(refreshDoneMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("refresh did not produce a result")
	}
}

func TestAppStatusBarShowsLogin(t *testing.T) {
	d, _ := deps(t)
	lines := strings.Split(sized(newApp(context.Background(), d)).View(), "\n")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "me") {
		t.Fatalf("status bar %q", last)
	}
}

func TestAppQuit(t *testing.T) {
	d, _ := deps(t)
	m := sized(newApp(context.Background(), d))
	_, cmd := m.Update(tuitest.Key("q"))
	if out := tuitest.Run(cmd); len(out) != 1 || out[0] != (tea.QuitMsg{}) {
		t.Fatalf("msgs = %+v", out)
	}
}

// drive feeds msg to the app and keeps feeding whatever its commands return.
func drive(m tea.Model, msg tea.Msg) tea.Model {
	queue := []tea.Msg{msg}
	for len(queue) > 0 && len(queue) < 50 {
		next, cmd := m.Update(queue[0])
		m, queue = next, queue[1:]
		for _, out := range tuitest.Run(cmd) {
			if _, tick := out.(tickMsg); !tick {
				queue = append(queue, out)
			}
		}
	}
	return m
}

func TestAppOpensThreadAndBack(t *testing.T) {
	d, f := deps(t)
	id := forge.ItemID{Repo: repoA, Number: 7}
	f.Threads[id] = forge.Thread{Item: forge.Item{ID: id, Title: "The thread"}, Body: "Body text"}
	m := drive(sized(newApp(context.Background(), d)), msgs.OpenThread{ID: id})
	if !strings.Contains(m.View(), "Body text") {
		t.Fatalf("thread not shown:\n%s", m.View())
	}
	m = drive(m, tuitest.Key("q"))
	if strings.Contains(m.View(), "Body text") {
		t.Fatalf("still on the thread:\n%s", m.View())
	}
}

func TestAppRefreshesAfterThread(t *testing.T) {
	d, f := deps(t)
	id := forge.ItemID{Repo: repoA, Number: 7}
	f.Threads[id] = forge.Thread{Item: forge.Item{ID: id}}
	m := drive(sized(newApp(context.Background(), d)), msgs.OpenThread{ID: id})
	before := len(f.CallLog())
	m = drive(m, tuitest.Key("q"))
	if !slices.Contains(f.CallLog()[before:], "ListMyPRs") || !strings.Contains(m.View(), "refreshed") {
		t.Fatalf("no refresh after leaving the thread; calls %v", f.CallLog()[before:])
	}
}

func TestAppQInThreadDoesNotQuit(t *testing.T) {
	d, f := deps(t)
	id := forge.ItemID{Repo: repoA, Number: 7}
	f.Threads[id] = forge.Thread{Item: forge.Item{ID: id}}
	m := drive(sized(newApp(context.Background(), d)), msgs.OpenThread{ID: id})
	_, cmd := m.Update(tuitest.Key("q"))
	for _, out := range tuitest.Run(cmd) {
		if out == (tea.QuitMsg{}) {
			t.Fatal("q in the thread view quit the app")
		}
	}
}

func TestAppTriageOverlayAndDone(t *testing.T) {
	d, f := deps(t)
	id := forge.ItemID{Repo: repoA, Number: 7}
	f.Threads[id] = forge.Thread{Item: forge.Item{ID: id}}
	f.Meta[repoA] = forge.RepoMeta{Labels: []forge.Label{{Name: "bug"}}}
	m := drive(sized(newApp(context.Background(), d)), msgs.OpenTriage{IDs: []forge.ItemID{id}})
	if !strings.Contains(m.View(), "Triage 1 item") {
		t.Fatalf("panel not shown:\n%s", m.View())
	}
	for _, k := range []string{"enter", "space", "esc", "a"} {
		m = drive(m, tuitest.Key(k))
	}
	if !strings.Contains(m.View(), "ok o/a#7") {
		t.Fatalf("not applied:\n%s", m.View())
	}
	m = drive(m, tuitest.Key("esc"))
	if strings.Contains(m.View(), "Triage 1 item") {
		t.Fatalf("panel still open:\n%s", m.View())
	}
	th, _ := f.Thread(t.Context(), id)
	if len(th.Item.Labels) != 1 {
		t.Fatalf("labels = %+v", th.Item.Labels)
	}
}

func TestAppNewIssueCreated(t *testing.T) {
	d, _ := deps(t)
	m := drive(sized(newApp(context.Background(), d)), msgs.NewIssue{})
	if !strings.Contains(m.View(), "New issue") || !strings.Contains(m.View(), "o/a") {
		t.Fatalf("form not shown:\n%s", m.View())
	}
	for _, k := range tuitest.Type("Hello") {
		m = drive(m, k)
	}
	m = drive(m, tuitest.Key("ctrl+s"))
	view := m.View()
	if strings.Contains(view, "New issue") || !strings.Contains(view, "created https://github.com/o/a/issues/") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestInboxDOpensReview(t *testing.T) {
	d, f := deps(t)
	pr := forge.ItemID{Repo: repoA, Number: 9}
	f.Incoming = []forge.Item{{ID: pr, IsPR: true, Title: "Some PR", Checks: &forge.Checks{State: forge.ChecksPassing}}}
	f.Diffs[pr] = forge.Diff{PR: pr, HeadSHA: "h", Files: []forge.FilePatch{{Path: "diffed.go", Patch: "@@ -1 +1 @@\n+x"}}}
	var m tea.Model = sized(newApp(context.Background(), d))
	m = drive(m, refreshDoneMsg(d.Refresher.Refresh(context.Background())))
	m = drive(m, tuitest.Key("d"))
	if !strings.Contains(m.View(), "diffed.go") {
		t.Fatalf("review not shown:\n%s", m.View())
	}
}

func TestInboxDOnIssueSaysPRsOnly(t *testing.T) {
	d, f := deps(t)
	f.Incoming = []forge.Item{{ID: forge.ItemID{Repo: repoA, Number: 9}, Title: "An issue"}}
	var m tea.Model = sized(newApp(context.Background(), d))
	m = drive(m, refreshDoneMsg(d.Refresher.Refresh(context.Background())))
	m = drive(m, tuitest.Key("d"))
	if !strings.Contains(m.View(), "d works on pull requests") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestAppOpenURLShowsURL(t *testing.T) {
	d, _ := deps(t)
	m := sized(newApp(context.Background(), d))
	next, _ := m.Update(msgs.OpenURL{URL: "https://github.com/o/a/issues/1"})
	if !strings.Contains(next.View(), "https://github.com/o/a/issues/1") {
		t.Fatalf("view:\n%s", next.View())
	}
}
