package github

import (
	"context"
	"strings"
	"time"

	"github.com/raven-clown/bridge/forge"
)

const fullComments = `allComments: comments(first: 100) { nodes { id author { login } createdAt body } }`

const commonEvents = `__typename
	... on LabeledEvent { createdAt actor { login } label { name } }
	... on UnlabeledEvent { createdAt actor { login } label { name } }
	... on MilestonedEvent { createdAt actor { login } milestoneTitle }
	... on AssignedEvent { createdAt actor { login } assignee { ... on User { login } } }
	... on ClosedEvent { createdAt actor { login } }
	... on ReopenedEvent { createdAt actor { login } }`

const prEvents = commonEvents + `
	... on MergedEvent { createdAt actor { login } }
	... on ReviewRequestedEvent { createdAt actor { login } requestedReviewer { ... on User { login } ... on Team { name } } }`

const threadQuery = `query($owner: String!, $name: String!, $number: Int!) {
	repository(owner: $owner, name: $name) {
		issueOrPullRequest(number: $number) {
			__typename
			... on Issue {` + commonFields + ` body ` + fullComments + `
				timelineItems(first: 100, itemTypes: [LABELED_EVENT, UNLABELED_EVENT, MILESTONED_EVENT, ASSIGNED_EVENT, CLOSED_EVENT, REOPENED_EVENT]) {
					nodes { ` + commonEvents + ` }
				}
			}
			... on PullRequest {` + prFields + ` body ` + fullComments + `
				reviewThreads(first: 100) { nodes { id path line isResolved comments(first: 50) { nodes { id author { login } createdAt body } } } }
				timelineItems(first: 100, itemTypes: [LABELED_EVENT, UNLABELED_EVENT, MILESTONED_EVENT, ASSIGNED_EVENT, CLOSED_EVENT, REOPENED_EVENT, MERGED_EVENT, REVIEW_REQUESTED_EVENT]) {
					nodes { ` + prEvents + ` }
				}
			}
		}
	}
}`

type fullComment struct {
	ID     string `json:"id"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	Body      string    `json:"body"`
}

func (c fullComment) toForge() forge.Comment {
	login := "ghost"
	if c.Author != nil {
		login = c.Author.Login
	}
	return forge.Comment{ID: forge.CommentID(c.ID), Author: forge.User{Login: login}, At: c.CreatedAt, Body: c.Body}
}

type threadNode struct {
	gqlNode
	Body        string `json:"body"`
	AllComments struct {
		Nodes []fullComment `json:"nodes"`
	} `json:"allComments"`
	ReviewThreads struct {
		Nodes []struct {
			ID         string `json:"id"`
			Path       string `json:"path"`
			Line       int    `json:"line"`
			IsResolved bool   `json:"isResolved"`
			Comments   struct {
				Nodes []fullComment `json:"nodes"`
			} `json:"comments"`
		} `json:"nodes"`
	} `json:"reviewThreads"`
	TimelineItems struct {
		Nodes []struct {
			Typename  string    `json:"__typename"`
			CreatedAt time.Time `json:"createdAt"`
			Actor     *struct {
				Login string `json:"login"`
			} `json:"actor"`
			Label *struct {
				Name string `json:"name"`
			} `json:"label"`
			MilestoneTitle string `json:"milestoneTitle"`
			Assignee       *struct {
				Login string `json:"login"`
			} `json:"assignee"`
			RequestedReviewer *struct {
				Login string `json:"login"`
				Name  string `json:"name"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"timelineItems"`
}

var eventKinds = map[string]string{
	"LabeledEvent": "labeled", "UnlabeledEvent": "unlabeled", "MilestonedEvent": "milestoned",
	"AssignedEvent": "assigned", "ClosedEvent": "closed", "ReopenedEvent": "reopened",
	"MergedEvent": "merged", "ReviewRequestedEvent": "review_requested",
}

func itemVars(id forge.ItemID) map[string]any {
	return map[string]any{"owner": id.Repo.Owner, "name": id.Repo.Name, "number": id.Number}
}

func (c *Client) Thread(ctx context.Context, id forge.ItemID) (forge.Thread, error) {
	var out struct {
		Repository *struct {
			IssueOrPullRequest *threadNode `json:"issueOrPullRequest"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, threadQuery, itemVars(id), &out); err != nil {
		return forge.Thread{}, err
	}
	if out.Repository == nil || out.Repository.IssueOrPullRequest == nil {
		return forge.Thread{}, forge.ErrNotFound
	}
	n := out.Repository.IssueOrPullRequest
	th := forge.Thread{Item: c.toItem(n.gqlNode), Body: n.Body}
	for _, cm := range n.AllComments.Nodes {
		th.Comments = append(th.Comments, cm.toForge())
	}
	for _, rt := range n.ReviewThreads.Nodes {
		t := forge.ReviewThread{ID: forge.CommentID(rt.ID), Path: rt.Path, Line: rt.Line, Resolved: rt.IsResolved}
		for _, cm := range rt.Comments.Nodes {
			t.Comments = append(t.Comments, cm.toForge())
		}
		th.ReviewThreads = append(th.ReviewThreads, t)
	}
	for _, ev := range n.TimelineItems.Nodes {
		kind, ok := eventKinds[ev.Typename]
		if !ok {
			continue
		}
		e := forge.Event{At: ev.CreatedAt, Kind: kind}
		if ev.Actor != nil {
			e.Actor = forge.User{Login: ev.Actor.Login}
		}
		switch {
		case ev.Label != nil:
			e.Detail = ev.Label.Name
		case ev.MilestoneTitle != "":
			e.Detail = ev.MilestoneTitle
		case ev.Assignee != nil:
			e.Detail = ev.Assignee.Login
		case ev.RequestedReviewer != nil:
			e.Detail = ev.RequestedReviewer.Login + ev.RequestedReviewer.Name
		}
		th.Events = append(th.Events, e)
	}
	// Projects need their own scope, so a failure here leaves them empty.
	if refs, err := c.itemProjects(ctx, id); err == nil {
		th.Item.Projects = refs
	}
	return th, nil
}

const addReplyMutation = `mutation($thread: ID!, $body: String!) {
	addPullRequestReviewThreadReply(input: {pullRequestReviewThreadId: $thread, body: $body}) { comment { id } }
}`

// Comment posts on the issue or pull request, or answers inside a review
// thread when replyTo is a review thread id.
func (c *Client) Comment(ctx context.Context, id forge.ItemID, body string, replyTo *forge.CommentID) (forge.CommentID, error) {
	if replyTo != nil && strings.HasPrefix(string(*replyTo), "PRRT_") {
		var out struct {
			Reply struct {
				Comment struct {
					ID string `json:"id"`
				} `json:"comment"`
			} `json:"addPullRequestReviewThreadReply"`
		}
		if err := c.graphql(ctx, addReplyMutation, map[string]any{"thread": string(*replyTo), "body": body}, &out); err != nil {
			return "", err
		}
		return forge.CommentID(out.Reply.Comment.ID), nil
	}
	var out struct {
		NodeID string `json:"node_id"`
	}
	if err := c.rest(ctx, "POST", issuePath(id)+"/comments", map[string]any{"body": body}, &out); err != nil {
		return "", err
	}
	return forge.CommentID(out.NodeID), nil
}
