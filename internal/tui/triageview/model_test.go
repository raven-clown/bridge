package triageview

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/raven-clown/bridge/config"
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
	repoA = forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}
	repoB = forge.RepoID{Host: "github.com", Owner: "o", Name: "b"}
)

func id(r forge.RepoID, n int) forge.ItemID { return forge.ItemID{Repo: r, Number: n} }

func fake() *forgetest.Fake {
	f := forgetest.New()
	for _, i := range []forge.ItemID{id(repoA, 1), id(repoA, 2), id(repoB, 3)} {
		f.Threads[i] = forge.Thread{Item: forge.Item{ID: i}}
	}
	meta := forge.RepoMeta{
		Labels:     []forge.Label{{Name: "bug"}, {Name: "docs"}, {Name: "wontfix"}},
		Milestones: []forge.Milestone{{Number: 1, Title: "v0.1", Open: true}},
		Assignable: []forge.User{{Login: "me"}, {Login: "pal"}},
		Projects: []forge.Project{{ID: "PVT_1", Title: "Board", Fields: []forge.ProjectField{
			{ID: "F_status", Name: "Status", Options: []forge.FieldOption{{ID: "O_todo", Name: "Todo"}, {ID: "O_done", Name: "Done"}}},
		}}},
	}
	f.Meta[repoA] = meta
	metaB := meta
	metaB.Labels = []forge.Label{{Name: "bug"}, {Name: "question"}}
	f.Meta[repoB] = metaB
	return f
}

func open(t *testing.T, f *forgetest.Fake, ids ...forge.ItemID) Model {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(t.TempDir(), "none.yaml"), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	m := New(ids, Deps{Forge: f, Store: s, Config: cfg}, 40, 24)
	return settle(m, m.Init())
}

// settle feeds the model every message its commands produce.
func settle(m Model, cmd tea.Cmd) Model {
	queue := tuitest.Run(cmd)
	for len(queue) > 0 {
		var next tea.Cmd
		m, next = m.Update(queue[0])
		queue = append(queue[1:], tuitest.Run(next)...)
	}
	return m
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

func labels(t *testing.T, f *forgetest.Fake, i forge.ItemID) []string {
	t.Helper()
	th, _ := f.Thread(t.Context(), i)
	var out []string
	for _, l := range th.Item.Labels {
		out = append(out, l.Name)
	}
	return out
}

func TestTriageGolden(t *testing.T) {
	m := open(t, fake(), id(repoA, 1), id(repoA, 2))
	tuitest.Golden(t, "triage_basic", m.View())
}

func TestTriageApplyTwoItems(t *testing.T) {
	f := fake()
	m := open(t, f, id(repoA, 1), id(repoA, 2))
	m, _ = press(m, "enter", "space", "esc", "a")
	view := m.View()
	if strings.Count(view, "ok o/a#") != 2 {
		t.Fatalf("view:\n%s", view)
	}
	for _, n := range []int{1, 2} {
		if !slices.Equal(labels(t, f, id(repoA, n)), []string{"bug"}) {
			t.Errorf("#%d labels = %v", n, labels(t, f, id(repoA, n)))
		}
	}
	_, out := press(m, "esc")
	if len(out) != 1 || out[0] != (msgs.TriageDone{}) {
		t.Fatalf("msgs = %+v", out)
	}
}

func TestTriageRemoveLabel(t *testing.T) {
	f := fake()
	th := f.Threads[id(repoA, 1)]
	th.Item.Labels = []forge.Label{{Name: "wontfix"}}
	f.Threads[id(repoA, 1)] = th
	m := open(t, f, id(repoA, 1))
	m, _ = press(m, "enter", "down", "down", "space", "space", "esc")
	if !strings.Contains(m.View(), "-wontfix") {
		t.Fatalf("view:\n%s", m.View())
	}
	_, _ = press(m, "a")
	if got := labels(t, f, id(repoA, 1)); len(got) != 0 {
		t.Fatalf("labels = %v", got)
	}
}

func TestTriageLabelPickerFilters(t *testing.T) {
	m := open(t, fake(), id(repoA, 1))
	m, _ = press(m, "enter")
	for _, k := range tuitest.Type("won") {
		m, _ = m.Update(k)
	}
	view := m.View()
	if !strings.Contains(view, "wontfix") || strings.Contains(view, "docs") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestTriageLabelsAreTheIntersection(t *testing.T) {
	m := open(t, fake(), id(repoA, 1), id(repoB, 3))
	m, _ = press(m, "enter")
	view := m.View()
	if !strings.Contains(view, "bug") || strings.Contains(view, "docs") || strings.Contains(view, "question") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestTriageMilestoneAndProject(t *testing.T) {
	f := fake()
	m := open(t, f, id(repoA, 1))
	m, _ = press(m, "down", "enter", "enter", "down", "down", "enter", "enter", "down", "enter", "a")
	th, _ := f.Thread(t.Context(), id(repoA, 1))
	if th.Item.Milestone == nil || th.Item.Milestone.Number != 1 {
		t.Fatalf("milestone = %+v\n%s", th.Item.Milestone, m.View())
	}
	items := f.ProjectItemsBy["PVT_1"]
	if len(items) != 1 || items[0].Fields["F_status"] != "O_done" {
		t.Fatalf("project items = %+v\n%s", items, m.View())
	}
}

func TestTriageProjectsScopeMessage(t *testing.T) {
	f := fake()
	meta := f.Meta[repoA]
	meta.Projects, meta.ProjectsErr = nil, forge.ErrProjectsScope
	f.Meta[repoA] = meta
	m := open(t, f, id(repoA, 1))
	if !strings.Contains(m.View(), "need the read:project scope") {
		t.Fatalf("view:\n%s", m.View())
	}
	m, _ = press(m, "down", "down", "down", "enter")
	if strings.Contains(m.View(), "Board") {
		t.Fatal("project picker opened without scope")
	}
}

func TestTriagePartialFailure(t *testing.T) {
	f := fake()
	delete(f.Threads, id(repoA, 2))
	m := open(t, f, id(repoA, 1), id(repoA, 2))
	m, _ = press(m, "enter", "space", "esc", "a")
	view := m.View()
	if !strings.Contains(view, "ok o/a#1") || !strings.Contains(view, "fail o/a#2") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestTriageCloseAsksConfirm(t *testing.T) {
	f := fake()
	m := open(t, f, id(repoA, 1))
	m, _ = press(m, "down", "down", "down", "down", "enter")
	if !strings.Contains(m.View(), "close") {
		t.Fatalf("state not set:\n%s", m.View())
	}
	m, _ = press(m, "a")
	if !strings.Contains(m.View(), "close 1 item? y/n") {
		t.Fatalf("no prompt:\n%s", m.View())
	}
	m, _ = press(m, "n")
	if slices.Contains(f.CallLog(), "SetState") {
		t.Fatal("closed after n")
	}
	_, _ = press(m, "a", "y")
	if f.States[id(repoA, 1)] != forge.StateClosed {
		t.Fatal("not closed after y")
	}
}

func TestMetaForAnotherRepoIsIgnored(t *testing.T) {
	f := fake()
	s, _ := store.Open(filepath.Join(t.TempDir(), "state"))
	m := New([]forge.ItemID{id(repoA, 1)}, Deps{Forge: f, Store: s}, 40, 24)
	m, _ = m.Update(metaMsg{repo: repoB, meta: f.Meta[repoB]})
	m = settle(m, m.Init())
	if !strings.Contains(m.View(), "Labels") {
		t.Fatalf("stuck loading:\n%s", m.View())
	}
}

func TestPickerScrollsWithinHeight(t *testing.T) {
	f := fake()
	meta := f.Meta[repoA]
	meta.Labels = nil
	for i := range 50 {
		meta.Labels = append(meta.Labels, forge.Label{Name: fmt.Sprintf("label-%02d", i)})
	}
	f.Meta[repoA] = meta
	s, _ := store.Open(filepath.Join(t.TempDir(), "state"))
	m := New([]forge.ItemID{id(repoA, 1)}, Deps{Forge: f, Store: s}, 40, 20)
	m = settle(m, m.Init())
	m, _ = press(m, "enter")
	for range 40 {
		m, _ = press(m, "down")
	}
	view := m.View()
	if n := len(strings.Split(view, "\n")); n > 20 {
		t.Fatalf("panel is %d lines tall:\n%s", n, view)
	}
	if !strings.Contains(view, "> [ ] label-40") || !strings.Contains(view, "Triage 1 item") {
		t.Fatalf("cursor or header not visible:\n%s", view)
	}
}

func TestTriageEscCancels(t *testing.T) {
	m := open(t, fake(), id(repoA, 1))
	_, out := press(m, "esc")
	if len(out) != 1 || out[0] != (msgs.TriageDone{}) {
		t.Fatalf("msgs = %+v", out)
	}
}
