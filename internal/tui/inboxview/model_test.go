package inboxview

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/inbox"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/tuitest"
)

func TestMain(m *testing.M) {
	tuitest.NoColor()
	os.Exit(m.Run())
}

var (
	now     = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	airlock = forge.RepoID{Host: "github.com", Owner: "raven-clown", Name: "airlock"}
	logbook = forge.RepoID{Host: "github.com", Owner: "raven-clown", Name: "logbook"}
)

func it(kind forge.InboxKind, repo forge.RepoID, n int, title string, u inbox.Urgency, ago time.Duration) inbox.Item {
	return inbox.Item{Kind: kind, Repo: repo, ID: forge.ItemID{Repo: repo, Number: n}, Title: title, Author: forge.User{Login: "someone"}, Urgency: u, UpdatedAt: now.Add(-ago), URL: "https://github.com/x/" + title}
}

func sample() inbox.Result {
	passing := &forge.Checks{State: forge.ChecksPassing}
	waiting := &forge.Checks{State: forge.ChecksAwaitingApproval}
	items := []inbox.Item{
		it(forge.IncomingPR, airlock, 31, "Fix the router", inbox.UrgencyForkRunAwaitingApproval, 2*time.Hour),
		it(forge.Mention, logbook, 12, "Question about recall", inbox.UrgencyUnanswered, 30*time.Minute),
		it(forge.ReviewRequest, logbook, 40, "Add verify tool", inbox.UrgencyReviewRequested, 3*24*time.Hour),
		it(forge.IncomingIssue, airlock, 33, "Crash on start", inbox.UrgencyUntriagedIssue, 5*time.Hour),
		it(forge.OwnPR, airlock, 35, "Rate limit profiles", inbox.UrgencyOwnPRWaiting, 26*time.Hour),
		{Kind: forge.PushedBranch, Repo: logbook, ID: forge.ItemID{Repo: logbook}, Branch: "feat/export", Title: "feat/export", Urgency: inbox.UrgencyPushedBranch, UpdatedAt: now.Add(-time.Hour)},
	}
	items[0].Checks = waiting
	items[1].Unanswered = 2
	items[4].Checks = passing
	return inbox.Result{Items: items, At: now}
}

func model() Model {
	return New(100, 30, func() time.Time { return now }).SetResult(sample(), false)
}

func press(m Model, keys ...string) (Model, []tea.Msg) {
	var out []tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(tuitest.Key(k))
		out = append(out, tuitest.Run(cmd)...)
	}
	return m, out
}

func TestInboxGolden(t *testing.T) {
	tuitest.Golden(t, "inbox_basic", model().View())
}

func TestInboxEmpty(t *testing.T) {
	m := New(100, 30, func() time.Time { return now }).SetResult(inbox.Result{At: now}, false)
	if !strings.Contains(m.View(), "Nothing is waiting for you") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestInboxProblemRowAndBanners(t *testing.T) {
	res := sample()
	res.Problems = []inbox.Problem{{Source: "mentions", Err: errors.New("boom")}}
	res.Truncated = []string{"incoming"}
	m := New(100, 30, func() time.Time { return now }).SetResult(res, true)
	lines := strings.Split(m.View(), "\n")
	if !strings.Contains(lines[0], "mentions: boom") {
		t.Fatalf("first line %q", lines[0])
	}
	view := m.View()
	if !strings.Contains(view, "incoming has more than 1000 results") || !strings.Contains(view, "cached") {
		t.Fatalf("banners missing:\n%s", view)
	}
}

func TestInboxEnterOpensThread(t *testing.T) {
	_, out := press(model(), "down", "enter")
	if len(out) != 1 || out[0] != (msgs.OpenThread{ID: forge.ItemID{Repo: logbook, Number: 12}}) {
		t.Fatalf("msgs = %+v", out)
	}
}

func TestInboxEnterOnBranchDoesNothing(t *testing.T) {
	_, out := press(model(), "G", "enter")
	if len(out) != 1 {
		t.Fatalf("msgs = %+v", out)
	}
	if _, ok := out[0].(msgs.Status); !ok {
		t.Fatalf("want a status message, got %+v", out[0])
	}
}

func TestInboxSelectAndTriageMsg(t *testing.T) {
	m, _ := press(model(), "space", "down", "space")
	if !strings.Contains(m.View(), "2 selected") {
		t.Fatalf("view:\n%s", m.View())
	}
	_, out := press(m, "t")
	tr, ok := out[0].(msgs.OpenTriage)
	if !ok || len(tr.IDs) != 2 || tr.IDs[0].Number != 31 || tr.IDs[1].Number != 12 {
		t.Fatalf("msgs = %+v", out)
	}
}

func TestInboxTriageWithoutSelectionUsesCursor(t *testing.T) {
	_, out := press(model(), "down", "down", "t")
	tr, ok := out[0].(msgs.OpenTriage)
	if !ok || len(tr.IDs) != 1 || tr.IDs[0].Number != 40 {
		t.Fatalf("msgs = %+v", out)
	}
}

func TestInboxFilter(t *testing.T) {
	m, _ := press(model(), "/")
	for _, k := range tuitest.Type("airlock") {
		m, _ = m.Update(k)
	}
	m, _ = press(m, "enter")
	view := m.View()
	if strings.Contains(view, "logbook") || !strings.Contains(view, "Fix the router") || !strings.Contains(view, "Crash on start") {
		t.Fatalf("view:\n%s", view)
	}
	m, _ = press(m, "esc")
	if !strings.Contains(m.View(), "logbook") {
		t.Fatal("esc did not clear the filter")
	}
}

func TestInboxTabJumpsSection(t *testing.T) {
	m, _ := press(model(), "tab")
	_, out := press(m, "enter")
	if out[0] != (msgs.OpenThread{ID: forge.ItemID{Repo: logbook, Number: 12}}) {
		t.Fatalf("msgs = %+v", out)
	}
}

func TestInboxMuteHidesItem(t *testing.T) {
	m, _ := press(model(), "x")
	if strings.Contains(m.View(), "Fix the router") {
		t.Fatalf("muted item still shown:\n%s", m.View())
	}
	m = m.SetResult(sample(), false)
	if strings.Contains(m.View(), "Fix the router") {
		t.Fatal("mute did not survive a refresh")
	}
}

func TestInboxKeysEmitMessages(t *testing.T) {
	_, out := press(model(), "R", "n", "o")
	if len(out) != 3 || out[0] != (msgs.Refresh{}) || out[1] != (msgs.NewIssue{}) || out[2] != (msgs.OpenURL{URL: "https://github.com/x/Fix the router"}) {
		t.Fatalf("msgs = %+v", out)
	}
}

func TestInboxNarrowWidthKeepsLinesShort(t *testing.T) {
	m := New(60, 30, func() time.Time { return now }).SetResult(sample(), false)
	for _, line := range strings.Split(m.View(), "\n") {
		if w := len([]rune(line)); w > 60 {
			t.Fatalf("line is %d wide: %q", w, line)
		}
	}
}
