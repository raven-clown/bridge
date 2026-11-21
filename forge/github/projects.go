package github

import (
	"context"
	"fmt"
	"strconv"

	"github.com/raven-clown/bridge/forge"
)

const projectsQuery = `query($login: String!) {
	repositoryOwner(login: $login) {
		... on ProjectV2Owner {
			projectsV2(first: 50, orderBy: {field: UPDATED_AT, direction: DESC}) {
				nodes {
					id number title
					fields(first: 30) { nodes {
						__typename
						... on ProjectV2FieldCommon { id name }
						... on ProjectV2SingleSelectField { options { id name } }
					} }
				}
			}
		}
	}
}`

func (c *Client) ListProjects(ctx context.Context, owner string) ([]forge.Project, error) {
	var out struct {
		RepositoryOwner *struct {
			ProjectsV2 struct {
				Nodes []struct {
					ID     string `json:"id"`
					Number int    `json:"number"`
					Title  string `json:"title"`
					Fields struct {
						Nodes []struct {
							ID      string              `json:"id"`
							Name    string              `json:"name"`
							Options []forge.FieldOption `json:"options"`
						} `json:"nodes"`
					} `json:"fields"`
				} `json:"nodes"`
			} `json:"projectsV2"`
		} `json:"repositoryOwner"`
	}
	if err := c.graphql(ctx, projectsQuery, map[string]any{"login": owner}, &out); err != nil {
		return nil, err
	}
	if out.RepositoryOwner == nil {
		return nil, forge.ErrNotFound
	}
	var ps []forge.Project
	for _, p := range out.RepositoryOwner.ProjectsV2.Nodes {
		fp := forge.Project{ID: forge.ProjectID(p.ID), Number: p.Number, Title: p.Title}
		for _, f := range p.Fields.Nodes {
			fp.Fields = append(fp.Fields, forge.ProjectField{ID: forge.FieldID(f.ID), Name: f.Name, Options: f.Options})
		}
		ps = append(ps, fp)
	}
	return ps, nil
}

const fieldName = `field { ... on ProjectV2FieldCommon { name } }`

const projectItemsQuery = `query($id: ID!, $after: String) {
	node(id: $id) {
		... on ProjectV2 {
			items(first: 100, after: $after) {
				pageInfo { hasNextPage endCursor }
				nodes {
					id
					content {
						__typename
						... on Issue { number title repository { name owner { login } } }
						... on PullRequest { number title repository { name owner { login } } }
						... on DraftIssue { title }
					}
					fieldValues(first: 20) { nodes {
						__typename
						... on ProjectV2ItemFieldSingleSelectValue { name ` + fieldName + ` }
						... on ProjectV2ItemFieldTextValue { text ` + fieldName + ` }
						... on ProjectV2ItemFieldNumberValue { number ` + fieldName + ` }
						... on ProjectV2ItemFieldDateValue { date ` + fieldName + ` }
					} }
				}
			}
		}
	}
}`

func (c *Client) ProjectItems(ctx context.Context, project forge.ProjectID) ([]forge.ProjectItem, error) {
	var (
		items []forge.ProjectItem
		after any
	)
	for {
		var out struct {
			Node *struct {
				Items struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						ID      string `json:"id"`
						Content *struct {
							Number     int    `json:"number"`
							Title      string `json:"title"`
							Repository *struct {
								Name  string `json:"name"`
								Owner struct {
									Login string `json:"login"`
								} `json:"owner"`
							} `json:"repository"`
						} `json:"content"`
						FieldValues struct {
							Nodes []struct {
								Name   string   `json:"name"`
								Text   string   `json:"text"`
								Number *float64 `json:"number"`
								Date   string   `json:"date"`
								Field  *struct {
									Name string `json:"name"`
								} `json:"field"`
							} `json:"nodes"`
						} `json:"fieldValues"`
					} `json:"nodes"`
				} `json:"items"`
			} `json:"node"`
		}
		if err := c.graphql(ctx, projectItemsQuery, map[string]any{"id": string(project), "after": after}, &out); err != nil {
			return nil, err
		}
		if out.Node == nil {
			return nil, forge.ErrNotFound
		}
		for _, n := range out.Node.Items.Nodes {
			pi := forge.ProjectItem{ID: forge.ProjectItemID(n.ID), Fields: map[string]string{}}
			if ct := n.Content; ct != nil {
				pi.Title = ct.Title
				if ct.Repository != nil {
					pi.Item = forge.ItemID{Repo: forge.RepoID{Host: c.host, Owner: ct.Repository.Owner.Login, Name: ct.Repository.Name}, Number: ct.Number}
				}
			}
			for _, v := range n.FieldValues.Nodes {
				if v.Field == nil {
					continue
				}
				switch {
				case v.Name != "":
					pi.Fields[v.Field.Name] = v.Name
				case v.Text != "":
					pi.Fields[v.Field.Name] = v.Text
				case v.Number != nil:
					pi.Fields[v.Field.Name] = strconv.FormatFloat(*v.Number, 'f', -1, 64)
				case v.Date != "":
					pi.Fields[v.Field.Name] = v.Date
				}
			}
			items = append(items, pi)
		}
		if !out.Node.Items.PageInfo.HasNextPage {
			return items, nil
		}
		after = out.Node.Items.PageInfo.EndCursor
	}
}

const contentIDQuery = `query($owner: String!, $name: String!, $number: Int!) {
	repository(owner: $owner, name: $name) {
		issueOrPullRequest(number: $number) { ... on Issue { id } ... on PullRequest { id } }
	}
}`

func (c *Client) contentID(ctx context.Context, id forge.ItemID) (string, error) {
	var out struct {
		Repository *struct {
			IssueOrPullRequest *struct {
				ID string `json:"id"`
			} `json:"issueOrPullRequest"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, contentIDQuery, itemVars(id), &out); err != nil {
		return "", err
	}
	if out.Repository == nil || out.Repository.IssueOrPullRequest == nil {
		return "", forge.ErrNotFound
	}
	return out.Repository.IssueOrPullRequest.ID, nil
}

const addItemMutation = `mutation($project: ID!, $content: ID!) {
	addProjectV2ItemById(input: {projectId: $project, contentId: $content}) { item { id } }
}`

func (c *Client) AddToProject(ctx context.Context, project forge.ProjectID, id forge.ItemID) (forge.ProjectItemID, error) {
	content, err := c.contentID(ctx, id)
	if err != nil {
		return "", err
	}
	var out struct {
		Add struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"addProjectV2ItemById"`
	}
	if err := c.graphql(ctx, addItemMutation, map[string]any{"project": string(project), "content": content}, &out); err != nil {
		return "", err
	}
	return forge.ProjectItemID(out.Add.Item.ID), nil
}

const updateFieldMutation = `mutation($project: ID!, $item: ID!, $field: ID!, $value: ProjectV2FieldValue!) {
	updateProjectV2ItemFieldValue(input: {projectId: $project, itemId: $item, fieldId: $field, value: $value}) { projectV2Item { id } }
}`

func (c *Client) SetProjectField(ctx context.Context, project forge.ProjectID, item forge.ProjectItemID, field forge.FieldID, v forge.FieldValue) error {
	value := map[string]any{}
	switch {
	case v.OptionID != "":
		value["singleSelectOptionId"] = v.OptionID
	case v.Number != nil:
		value["number"] = *v.Number
	case v.Date != nil:
		value["date"] = v.Date.Format("2006-01-02")
	case v.Text != "":
		value["text"] = v.Text
	default:
		return fmt.Errorf("github: empty value for field %s", field)
	}
	vars := map[string]any{"project": string(project), "item": string(item), "field": string(field), "value": value}
	return c.graphql(ctx, updateFieldMutation, vars, nil)
}

const itemProjectsQuery = `query($owner: String!, $name: String!, $number: Int!) {
	repository(owner: $owner, name: $name) {
		issueOrPullRequest(number: $number) {
			... on Issue { projectItems(first: 10) { nodes { id project { id } fieldValueByName(name: "Status") { ... on ProjectV2ItemFieldSingleSelectValue { name } } } } }
			... on PullRequest { projectItems(first: 10) { nodes { id project { id } fieldValueByName(name: "Status") { ... on ProjectV2ItemFieldSingleSelectValue { name } } } } }
		}
	}
}`

func (c *Client) itemProjects(ctx context.Context, id forge.ItemID) ([]forge.ProjectRef, error) {
	var out struct {
		Repository *struct {
			IssueOrPullRequest *struct {
				ProjectItems struct {
					Nodes []struct {
						ID      string `json:"id"`
						Project struct {
							ID string `json:"id"`
						} `json:"project"`
						FieldValueByName *struct {
							Name string `json:"name"`
						} `json:"fieldValueByName"`
					} `json:"nodes"`
				} `json:"projectItems"`
			} `json:"issueOrPullRequest"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, itemProjectsQuery, itemVars(id), &out); err != nil {
		return nil, err
	}
	if out.Repository == nil || out.Repository.IssueOrPullRequest == nil {
		return nil, nil
	}
	var refs []forge.ProjectRef
	for _, n := range out.Repository.IssueOrPullRequest.ProjectItems.Nodes {
		r := forge.ProjectRef{ProjectID: forge.ProjectID(n.Project.ID), ItemID: forge.ProjectItemID(n.ID)}
		if n.FieldValueByName != nil {
			r.Column = n.FieldValueByName.Name
		}
		refs = append(refs, r)
	}
	return refs, nil
}
