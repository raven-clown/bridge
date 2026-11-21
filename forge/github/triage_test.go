package github

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/raven-clown/bridge/forge"
)

func metaData() map[string]any {
	return map[string]any{"repository": map[string]any{
		"labels":          map[string]any{"nodes": []any{map[string]any{"name": "bug", "color": "d73a4a"}, map[string]any{"name": "good first issue", "color": "7057ff"}}},
		"milestones":      map[string]any{"nodes": []any{map[string]any{"id": "MI_1", "number": 1, "title": "v0.1", "dueOn": "2026-12-01T00:00:00Z", "state": "OPEN"}}},
		"assignableUsers": map[string]any{"nodes": []any{map[string]any{"login": "me"}, map[string]any{"login": "pal"}}},
	}}
}

func projectsData() map[string]any {
	project := map[string]any{"id": "PVT_1", "number": 4, "title": "bridge", "fields": map[string]any{"nodes": []any{
		map[string]any{"__typename": "ProjectV2SingleSelectField", "id": "F_status", "name": "Status", "options": []any{
			map[string]any{"id": "O_todo", "name": "Todo"}, map[string]any{"id": "O_done", "name": "Done"},
		}},
		map[string]any{"__typename": "ProjectV2Field", "id": "F_title", "name": "Title"},
	}}}
	return map[string]any{"repositoryOwner": map[string]any{"projectsV2": map[string]any{"nodes": []any{project}}}}
}

func TestRepoMeta(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		if strings.Contains(r.Query, "projectsV2") {
			if r.Variables["login"] != "o" {
				t.Errorf("projects vars = %v", r.Variables)
			}
			return ok(projectsData())
		}
		return ok(metaData())
	}
	m, err := f.client().RepoMeta(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Labels) != 2 || len(m.Milestones) != 1 || m.Milestones[0].Number != 1 || len(m.Assignable) != 2 || m.ProjectsErr != nil {
		t.Fatalf("meta = %+v", m)
	}
	if len(m.Projects) != 1 || m.Projects[0].ID != "PVT_1" || len(m.Projects[0].Fields) != 2 || len(m.Projects[0].Fields[0].Options) != 2 {
		t.Fatalf("projects = %+v", m.Projects)
	}
}

func TestRepoMetaProjectsForbidden(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		if strings.Contains(r.Query, "projectsV2") {
			return scopeError()
		}
		return ok(metaData())
	}
	m, err := f.client().RepoMeta(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Labels) != 2 || !errors.Is(m.ProjectsErr, forge.ErrProjectsScope) || len(m.Projects) != 0 {
		t.Fatalf("meta = %+v", m)
	}
}

type restLog struct {
	mu    sync.Mutex
	calls []string
	body  map[string]map[string]any
}

func (l *restLog) route(key string, status int, out any) func(map[string]any) (int, any) {
	return func(b map[string]any) (int, any) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.calls = append(l.calls, key)
		if l.body == nil {
			l.body = map[string]map[string]any{}
		}
		l.body[key] = b
		return status, out
	}
}

func TestSetLabelsAddRemove(t *testing.T) {
	f := newFakeGH(t)
	log := &restLog{}
	f.REST = restMux(t, map[string]func(map[string]any) (int, any){
		"POST /repos/o/a/issues/42/labels":                        log.route("add", 200, []any{}),
		"DELETE /repos/o/a/issues/42/labels/good%20first%20issue": log.route("remove", 200, []any{}),
	})
	if err := f.client().SetLabels(context.Background(), pr42, []string{"bug"}, []string{"good first issue"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(log.calls, []string{"add", "remove"}) {
		t.Fatalf("calls = %v", log.calls)
	}
	if labels, _ := log.body["add"]["labels"].([]any); len(labels) != 1 || labels[0] != "bug" {
		t.Fatalf("add body = %v", log.body["add"])
	}
}

func TestSetLabelsRemoveMissingIsFine(t *testing.T) {
	f := newFakeGH(t)
	f.REST = restMux(t, map[string]func(map[string]any) (int, any){
		"DELETE /repos/o/a/issues/42/labels/bug": func(map[string]any) (int, any) { return 404, map[string]any{"message": "Label does not exist"} },
	})
	if err := f.client().SetLabels(context.Background(), pr42, nil, []string{"bug"}); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestSetMilestoneAndState(t *testing.T) {
	f := newFakeGH(t)
	log := &restLog{}
	f.REST = restMux(t, map[string]func(map[string]any) (int, any){"PATCH /repos/o/a/issues/42": log.route("patch", 200, map[string]any{})})
	c := f.client()
	m := forge.MilestoneID(3)
	if err := c.SetMilestone(context.Background(), pr42, &m); err != nil {
		t.Fatal(err)
	}
	if log.body["patch"]["milestone"] != float64(3) {
		t.Fatalf("milestone body = %v", log.body["patch"])
	}
	if err := c.SetMilestone(context.Background(), pr42, nil); err != nil {
		t.Fatal(err)
	}
	if v, present := log.body["patch"]["milestone"]; !present || v != nil {
		t.Fatalf("clear body = %v", log.body["patch"])
	}
	if err := c.SetState(context.Background(), pr42, forge.StateClosed); err != nil {
		t.Fatal(err)
	}
	if log.body["patch"]["state"] != "closed" {
		t.Fatalf("state body = %v", log.body["patch"])
	}
}

func TestSetAssignees(t *testing.T) {
	f := newFakeGH(t)
	log := &restLog{}
	f.REST = restMux(t, map[string]func(map[string]any) (int, any){
		"POST /repos/o/a/issues/42/assignees":   log.route("add", 201, map[string]any{}),
		"DELETE /repos/o/a/issues/42/assignees": log.route("remove", 200, map[string]any{}),
	})
	if err := f.client().SetAssignees(context.Background(), pr42, []string{"me"}, []string{"pal"}); err != nil {
		t.Fatal(err)
	}
	if a, _ := log.body["remove"]["assignees"].([]any); len(a) != 1 || a[0] != "pal" {
		t.Fatalf("remove body = %v", log.body["remove"])
	}
}

func TestCreateIssue(t *testing.T) {
	f := newFakeGH(t)
	log := &restLog{}
	f.REST = restMux(t, map[string]func(map[string]any) (int, any){"POST /repos/o/a/issues": log.route("create", 201, map[string]any{"number": 77})})
	m := forge.MilestoneID(1)
	id, err := f.client().CreateIssue(context.Background(), repoA, forge.NewIssue{Title: "T", Body: "B", Labels: []string{"bug"}, Assignees: []string{"me"}, Milestone: &m})
	if err != nil || id != (forge.ItemID{Repo: repoA, Number: 77}) {
		t.Fatalf("id %v err %v", id, err)
	}
	b := log.body["create"]
	if b["title"] != "T" || b["milestone"] != float64(1) {
		t.Fatalf("body = %v", b)
	}
}

func TestClientImplementsForge(t *testing.T) {
	var _ forge.Forge = (*Client)(nil)
}
