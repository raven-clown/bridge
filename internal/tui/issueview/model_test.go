package issueview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/forgetest"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/tuitest"
)

func TestMain(m *testing.M) {
	tuitest.NoColor()
	os.Exit(m.Run())
}

var (
	repoA = forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}
	repoB = forge.RepoID{Host: "github.com", Owner: "o", Name: "b"}
)

func setup(t *testing.T) (Model, *forgetest.Fake) {
	t.Helper()
	cfg, err := config.Load(filepath.Join(t.TempDir(), "none.yaml"), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Policy["default"]
	p.Sidebar = config.SidebarDefaults{Labels: []string{"needs-review"}, Milestone: "current", Assignee: "me"}
	cfg.Policy["default"] = p
	cfg.Policy["o/b"] = config.Policy{Sidebar: config.SidebarDefaults{Labels: []string{"triage"}}}

	f := forgetest.New()
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	f.Meta[repoA] = forge.RepoMeta{Milestones: []forge.Milestone{{Number: 1, Title: "v0.1", Due: &due, Open: true}}}
	return New([]forge.RepoID{repoA, repoB}, Deps{Forge: f, Config: cfg}, 80, 24), f
}

func press(m Model, keys ...string) (Model, []tea.Msg) {
	var out []tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(tuitest.Key(k))
		queue := tuitest.Run(cmd)
		for len(queue) > 0 {
			out = append(out, queue[0])
			var next tea.Cmd
			m, next = m.Update(queue[0])
			queue = append(queue[1:], tuitest.Run(next)...)
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

func TestIssueGolden(t *testing.T) {
	m, _ := setup(t)
	tuitest.Golden(t, "issue_basic", m.View())
}

func TestIssueFormDefaults(t *testing.T) {
	m, f := setup(t)
	if !strings.Contains(m.View(), "needs-review") || !strings.Contains(m.View(), "milestone current") {
		t.Fatalf("defaults not shown:\n%s", m.View())
	}
	m = typeText(m, "Add a thing")
	_, out := press(m, "ctrl+s")
	var created *msgs.IssueCreated
	for _, msg := range out {
		if c, ok := msg.(msgs.IssueCreated); ok {
			created = &c
		}
	}
	if created == nil || !strings.HasPrefix(created.URL, "https://github.com/o/a/issues/") {
		t.Fatalf("msgs = %+v", out)
	}
	th, _ := f.Thread(t.Context(), created.ID)
	it := th.Item
	if it.Title != "Add a thing" || len(it.Labels) != 1 || it.Labels[0].Name != "needs-review" ||
		len(it.Assignees) != 1 || it.Assignees[0].Login != "me" || it.Milestone == nil || it.Milestone.Number != 1 {
		t.Fatalf("issue = %+v", it)
	}
}

func TestIssueFormNeedsTitle(t *testing.T) {
	m, f := setup(t)
	m, _ = press(m, "ctrl+s")
	if !strings.Contains(m.View(), "an issue needs a title") || len(f.CallLog()) != 0 {
		t.Fatalf("view:\n%s\ncalls %v", m.View(), f.CallLog())
	}
}

func TestIssueRepoSwitchUpdatesLabels(t *testing.T) {
	m, _ := setup(t)
	m, _ = press(m, "shift+tab", "right")
	view := m.View()
	if !strings.Contains(view, "o/b") || !strings.Contains(view, "triage") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestIssueBodyAndLabelsEdited(t *testing.T) {
	m, f := setup(t)
	m = typeText(m, "T")
	m, _ = press(m, "tab")
	m = typeText(m, "the body")
	m, _ = press(m, "tab", "ctrl+u")
	m = typeText(m, "bug, docs")
	_, out := press(m, "ctrl+s")
	for _, msg := range out {
		if c, ok := msg.(msgs.IssueCreated); ok {
			th, _ := f.Thread(t.Context(), c.ID)
			if th.Body != "the body" || len(th.Item.Labels) != 2 || th.Item.Labels[1].Name != "docs" {
				t.Fatalf("issue = %+v", th)
			}
			return
		}
	}
	t.Fatalf("not created: %+v", out)
}

func TestIssueEscGoesBack(t *testing.T) {
	m, _ := setup(t)
	_, out := press(m, "esc")
	if len(out) != 1 || out[0] != (msgs.Back{}) {
		t.Fatalf("msgs = %+v", out)
	}
}
