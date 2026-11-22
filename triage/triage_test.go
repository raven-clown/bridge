package triage

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/forgetest"
)

var repo = forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}

func item(n int) forge.ItemID { return forge.ItemID{Repo: repo, Number: n} }

func fake() *forgetest.Fake {
	f := forgetest.New()
	for n := 1; n <= 3; n++ {
		f.Threads[item(n)] = forge.Thread{Item: forge.Item{ID: item(n)}}
	}
	soon := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	f.Meta[repo] = forge.RepoMeta{Milestones: []forge.Milestone{
		{Number: 2, Title: "v0.2", Due: &later, Open: true},
		{Number: 1, Title: "v0.1", Due: &soon, Open: true},
		{Number: 9, Title: "someday", Open: true},
	}}
	return f
}

func labels(t *testing.T, f *forgetest.Fake, id forge.ItemID) []string {
	t.Helper()
	th, err := f.Thread(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range th.Item.Labels {
		out = append(out, l.Name)
	}
	return out
}

func TestApplyLabelsToMany(t *testing.T) {
	f := fake()
	res := Apply(context.Background(), f, []forge.ItemID{item(1), item(2)}, Change{AddLabels: []string{"bug"}})
	if len(res) != 2 || res[0].Item != item(1) || res[1].Item != item(2) || res[0].Err != nil || res[1].Err != nil {
		t.Fatalf("results = %+v", res)
	}
	if !slices.Equal(labels(t, f, item(1)), []string{"bug"}) || !slices.Equal(labels(t, f, item(2)), []string{"bug"}) {
		t.Fatal("labels not applied")
	}
}

func TestApplyIndependentFailures(t *testing.T) {
	f := fake()
	delete(f.Threads, item(2))
	res := Apply(context.Background(), f, []forge.ItemID{item(1), item(2), item(3)}, Change{AddLabels: []string{"bug"}})
	if res[0].Err != nil || !errors.Is(res[1].Err, forge.ErrNotFound) || res[2].Err != nil {
		t.Fatalf("results = %+v", res)
	}
	if !slices.Equal(labels(t, f, item(3)), []string{"bug"}) {
		t.Fatal("third item skipped")
	}
}

func TestApplyResolvesMilestoneName(t *testing.T) {
	f := fake()
	name := "v0.2"
	res := Apply(context.Background(), f, []forge.ItemID{item(1)}, Change{Milestone: &name})
	if res[0].Err != nil {
		t.Fatal(res[0].Err)
	}
	th, _ := f.Thread(context.Background(), item(1))
	if th.Item.Milestone == nil || th.Item.Milestone.Number != 2 {
		t.Fatalf("milestone = %+v", th.Item.Milestone)
	}
}

func TestApplyUnknownMilestone(t *testing.T) {
	f := fake()
	name := "v9"
	res := Apply(context.Background(), f, []forge.ItemID{item(1)}, Change{Milestone: &name})
	if !errors.Is(res[0].Err, ErrUnknownMilestone) {
		t.Fatalf("got %v", res[0].Err)
	}
	if slices.Contains(f.CallLog(), "SetMilestone") {
		t.Fatal("SetMilestone called for an unknown name")
	}
}

func TestApplyClearMilestoneStateAssigneesProject(t *testing.T) {
	f := fake()
	closed := forge.StateClosed
	ch := Change{
		ClearMilestone:  true,
		AddAssignees:    []string{"me"},
		RemoveAssignees: []string{"pal"},
		State:           &closed,
		Project:         &ProjectMove{ProjectID: "PVT_1", FieldID: "F_status", OptionID: "O_done"},
	}
	res := Apply(context.Background(), f, []forge.ItemID{item(1)}, ch)
	if res[0].Err != nil {
		t.Fatal(res[0].Err)
	}
	if f.States[item(1)] != forge.StateClosed {
		t.Fatal("not closed")
	}
	items := f.ProjectItemsBy["PVT_1"]
	if len(items) != 1 || items[0].Fields["F_status"] != "O_done" {
		t.Fatalf("project items = %+v", items)
	}
	th, _ := f.Thread(context.Background(), item(1))
	if len(th.Item.Assignees) != 1 || th.Item.Assignees[0].Login != "me" || th.Item.Milestone != nil {
		t.Fatalf("item = %+v", th.Item)
	}
}

func TestApplyEmptyChangeCallsNothing(t *testing.T) {
	f := fake()
	res := Apply(context.Background(), f, []forge.ItemID{item(1)}, Change{})
	if res[0].Err != nil || len(f.CallLog()) != 0 {
		t.Fatalf("res %+v calls %v", res, f.CallLog())
	}
}

func loadConfig(t *testing.T) config.Config {
	t.Helper()
	c, err := config.Load(filepath.Join(t.TempDir(), "none.yaml"), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCreateIssueSidebarDefaults(t *testing.T) {
	f := fake()
	f.Projects["o"] = []forge.Project{{ID: "PVT_1", Title: "Roadmap"}}
	cfg := loadConfig(t)
	p := cfg.Policy["default"]
	p.Sidebar = config.SidebarDefaults{Labels: []string{"needs-review"}, Milestone: "current", Assignee: "me", Project: "Roadmap"}
	cfg.Policy["default"] = p

	id, err := CreateIssue(context.Background(), f, cfg, repo, IssueDraft{Title: "T", Body: "B"})
	if err != nil {
		t.Fatal(err)
	}
	th, _ := f.Thread(context.Background(), id)
	if len(th.Item.Labels) != 1 || th.Item.Labels[0].Name != "needs-review" {
		t.Fatalf("labels = %+v", th.Item.Labels)
	}
	if th.Item.Milestone == nil || th.Item.Milestone.Number != 1 {
		t.Fatalf("milestone = %+v", th.Item.Milestone)
	}
	if len(th.Item.Assignees) != 1 || th.Item.Assignees[0].Login != f.Me {
		t.Fatalf("assignees = %+v", th.Item.Assignees)
	}
	if items := f.ProjectItemsBy["PVT_1"]; len(items) != 1 || items[0].Item != id {
		t.Fatalf("project items = %+v", items)
	}
}

func TestCreateIssueExplicitValuesWin(t *testing.T) {
	f := fake()
	cfg := loadConfig(t)
	p := cfg.Policy["default"]
	p.Sidebar = config.SidebarDefaults{Labels: []string{"needs-review"}, Milestone: "current", Assignee: "me"}
	cfg.Policy["default"] = p
	id, err := CreateIssue(context.Background(), f, cfg, repo, IssueDraft{Title: "T", Labels: []string{"bug"}, Milestone: "v0.2", Assignees: []string{"pal"}})
	if err != nil {
		t.Fatal(err)
	}
	th, _ := f.Thread(context.Background(), id)
	if th.Item.Labels[0].Name != "bug" || th.Item.Milestone.Number != 2 || th.Item.Assignees[0].Login != "pal" {
		t.Fatalf("item = %+v", th.Item)
	}
}

func TestCreateIssueNeedsTitle(t *testing.T) {
	if _, err := CreateIssue(context.Background(), fake(), loadConfig(t), repo, IssueDraft{Title: " "}); !errors.Is(err, ErrNoTitle) {
		t.Fatalf("got %v", err)
	}
}

func TestCreateIssueUnknownProjectStillCreates(t *testing.T) {
	f := fake()
	cfg := loadConfig(t)
	p := cfg.Policy["default"]
	p.Sidebar.Project = "Nope"
	cfg.Policy["default"] = p
	id, err := CreateIssue(context.Background(), f, cfg, repo, IssueDraft{Title: "T"})
	if id.Number == 0 || !errors.Is(err, ErrUnknownProject) {
		t.Fatalf("id %v err %v", id, err)
	}
}
