package github

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/raven-clown/bridge/forge"
)

const metaQuery = `query($owner: String!, $name: String!) {
	repository(owner: $owner, name: $name) {
		labels(first: 100) { nodes { name color } }
		milestones(first: 50, states: [OPEN], orderBy: {field: DUE_DATE, direction: ASC}) { nodes { id number title dueOn state } }
		assignableUsers(first: 100) { nodes { login } }
	}
}`

// RepoMeta loads projects in a second query, so a token without project
// scope still gets labels, milestones and assignees.
func (c *Client) RepoMeta(ctx context.Context, repo forge.RepoID) (forge.RepoMeta, error) {
	var out struct {
		Repository *gqlNodeMeta `json:"repository"`
	}
	if err := c.graphql(ctx, metaQuery, map[string]any{"owner": repo.Owner, "name": repo.Name}, &out); err != nil {
		return forge.RepoMeta{}, err
	}
	if out.Repository == nil {
		return forge.RepoMeta{}, forge.ErrNotFound
	}
	var m forge.RepoMeta
	for _, l := range out.Repository.Labels.Nodes {
		m.Labels = append(m.Labels, forge.Label{Name: l.Name, Color: l.Color})
	}
	for _, ms := range out.Repository.Milestones.Nodes {
		m.Milestones = append(m.Milestones, forge.Milestone{ID: ms.ID, Number: ms.Number, Title: ms.Title, Due: ms.DueOn, Open: ms.State == "OPEN"})
	}
	for _, u := range out.Repository.AssignableUsers.Nodes {
		m.Assignable = append(m.Assignable, forge.User{Login: u.Login})
	}
	projects, err := c.ListProjects(ctx, repo.Owner)
	switch {
	case err == nil:
		m.Projects = projects
	case errors.Is(err, forge.ErrForbidden):
		m.ProjectsErr = forge.ErrProjectsScope
	default:
		m.ProjectsErr = err
	}
	return m, nil
}

type gqlNodeMeta struct {
	Labels struct {
		Nodes []struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"nodes"`
	} `json:"labels"`
	Milestones struct {
		Nodes []struct {
			ID     string     `json:"id"`
			Number int        `json:"number"`
			Title  string     `json:"title"`
			DueOn  *time.Time `json:"dueOn"`
			State  string     `json:"state"`
		} `json:"nodes"`
	} `json:"milestones"`
	AssignableUsers struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignableUsers"`
}

// SetLabels uses GitHub's add and remove endpoints rather than replacing
// the whole set, so a label someone else added meanwhile survives.
func (c *Client) SetLabels(ctx context.Context, id forge.ItemID, add, remove []string) error {
	if len(add) > 0 {
		if err := c.rest(ctx, http.MethodPost, issuePath(id)+"/labels", map[string]any{"labels": add}, nil); err != nil {
			return err
		}
	}
	for _, name := range remove {
		err := c.rest(ctx, http.MethodDelete, issuePath(id)+"/labels/"+url.PathEscape(name), nil, nil)
		if err != nil && !errors.Is(err, forge.ErrNotFound) {
			return err
		}
	}
	return nil
}

func (c *Client) SetAssignees(ctx context.Context, id forge.ItemID, add, remove []string) error {
	if len(add) > 0 {
		if err := c.rest(ctx, http.MethodPost, issuePath(id)+"/assignees", map[string]any{"assignees": add}, nil); err != nil {
			return err
		}
	}
	if len(remove) > 0 {
		if err := c.rest(ctx, http.MethodDelete, issuePath(id)+"/assignees", map[string]any{"assignees": remove}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) SetMilestone(ctx context.Context, id forge.ItemID, m *forge.MilestoneID) error {
	var v any
	if m != nil {
		v = int(*m)
	}
	return c.rest(ctx, http.MethodPatch, issuePath(id), map[string]any{"milestone": v}, nil)
}

func (c *Client) SetState(ctx context.Context, id forge.ItemID, s forge.ItemState) error {
	state := "open"
	if s == forge.StateClosed {
		state = "closed"
	}
	return c.rest(ctx, http.MethodPatch, issuePath(id), map[string]any{"state": state}, nil)
}

func (c *Client) CreateIssue(ctx context.Context, repo forge.RepoID, issue forge.NewIssue) (forge.ItemID, error) {
	body := map[string]any{"title": issue.Title, "body": issue.Body}
	if len(issue.Labels) > 0 {
		body["labels"] = issue.Labels
	}
	if len(issue.Assignees) > 0 {
		body["assignees"] = issue.Assignees
	}
	if issue.Milestone != nil {
		body["milestone"] = int(*issue.Milestone)
	}
	var out struct {
		Number int `json:"number"`
	}
	if err := c.rest(ctx, http.MethodPost, repoPath(repo)+"/issues", body, &out); err != nil {
		return forge.ItemID{}, err
	}
	return forge.ItemID{Repo: repo, Number: out.Number}, nil
}
