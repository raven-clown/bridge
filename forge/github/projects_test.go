package github

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/raven-clown/bridge/forge"
)

func TestListProjects(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) { return ok(projectsData()) }
	ps, err := f.client().ListProjects(context.Background(), "o")
	if err != nil || len(ps) != 1 || ps[0].Number != 4 || ps[0].Fields[0].Name != "Status" {
		t.Fatalf("projects %+v err %v", ps, err)
	}
}

func TestProjectItems(t *testing.T) {
	f := newFakeGH(t)
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		return ok(map[string]any{"node": map[string]any{"items": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false},
			"nodes": []any{
				map[string]any{
					"id":      "PVTI_1",
					"content": map[string]any{"__typename": "Issue", "number": 5, "title": "Crash", "repository": map[string]any{"name": "a", "owner": map[string]any{"login": "o"}}},
					"fieldValues": map[string]any{"nodes": []any{
						map[string]any{"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "Todo", "field": map[string]any{"name": "Status"}},
						map[string]any{"__typename": "ProjectV2ItemFieldTextValue", "text": "x", "field": map[string]any{"name": "Note"}},
					}},
				},
				map[string]any{"id": "PVTI_2", "content": map[string]any{"__typename": "DraftIssue", "title": "Idea"}, "fieldValues": map[string]any{"nodes": []any{}}},
			},
		}}})
	}
	items, err := f.client().ProjectItems(context.Background(), "PVT_1")
	if err != nil || len(items) != 2 {
		t.Fatalf("items %+v err %v", items, err)
	}
	if items[0].Item != (forge.ItemID{Repo: repoA, Number: 5}) || items[0].Fields["Status"] != "Todo" || items[0].Fields["Note"] != "x" {
		t.Fatalf("item 0 = %+v", items[0])
	}
	if items[1].Item.Number != 0 || items[1].Title != "Idea" {
		t.Fatalf("draft = %+v", items[1])
	}
}

func TestAddToProjectAndSetSingleSelect(t *testing.T) {
	f := newFakeGH(t)
	var mutations []gqlReq
	f.GraphQL = func(r gqlReq) (int, http.Header, any) {
		switch {
		case strings.Contains(r.Query, "issueOrPullRequest"):
			return ok(map[string]any{"repository": map[string]any{"issueOrPullRequest": map[string]any{"id": "PR_42"}}})
		case strings.Contains(r.Query, "addProjectV2ItemById"):
			mutations = append(mutations, r)
			return ok(map[string]any{"addProjectV2ItemById": map[string]any{"item": map[string]any{"id": "PVTI_9"}}})
		case strings.Contains(r.Query, "updateProjectV2ItemFieldValue"):
			mutations = append(mutations, r)
			return ok(map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": map[string]any{"id": "PVTI_9"}}})
		}
		t.Errorf("unexpected query %q", r.Query)
		return ok(nil)
	}
	c := f.client()
	item, err := c.AddToProject(context.Background(), "PVT_1", pr42)
	if err != nil || item != "PVTI_9" {
		t.Fatalf("item %q err %v", item, err)
	}
	if err := c.SetProjectField(context.Background(), "PVT_1", item, "F_status", forge.FieldValue{OptionID: "O_done"}); err != nil {
		t.Fatal(err)
	}
	if len(mutations) != 2 || mutations[0].Variables["content"] != "PR_42" {
		t.Fatalf("mutations = %+v", mutations)
	}
	value, _ := mutations[1].Variables["value"].(map[string]any)
	if value["singleSelectOptionId"] != "O_done" || mutations[1].Variables["field"] != "F_status" {
		t.Fatalf("update vars = %v", mutations[1].Variables)
	}
}
