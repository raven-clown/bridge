package github

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/raven-clown/bridge/forge"
)

const (
	pageSize     = 100
	searchCap    = 1000
	reposPerPage = 20
	branchWindow = 14 * 24 * time.Hour
)

const commonFields = `
	number title url updatedAt
	author { __typename login }
	repository { name owner { login } }
	labels(first: 20) { nodes { name color } }
	milestone { id number title dueOn state }
	assignees(first: 10) { nodes { login } }
	comments(last: 5) { nodes { id author { login } createdAt bodyText } }`

const prFields = commonFields + `
	headRefName isCrossRepository reviewDecision mergeable mergeStateStatus
	commits(last: 1) { nodes { commit {
		statusCheckRollup { state contexts(first: 100) { nodes {
			__typename
			... on CheckRun { status conclusion }
			... on StatusContext { state }
		} } }
		checkSuites(first: 20) { nodes { status conclusion } }
	} } }`

const nodeFields = `__typename
	... on Issue {` + commonFields + `}
	... on PullRequest {` + prFields + `}`

const searchQuery = `query($q: String!, $after: String) {
	search(query: $q, type: ISSUE, first: 100, after: $after) {
		issueCount
		pageInfo { hasNextPage endCursor }
		nodes { ` + nodeFields + ` }
	}
}`

type gqlComment struct {
	ID     string `json:"id"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	BodyText  string    `json:"bodyText"`
}

func (c gqlComment) login() string {
	if c.Author == nil {
		return "ghost"
	}
	return c.Author.Login
}

type gqlNode struct {
	Typename  string    `json:"__typename"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
	Author    *struct {
		Typename string `json:"__typename"`
		Login    string `json:"login"`
	} `json:"author"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Labels struct {
		Nodes []struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"nodes"`
	} `json:"labels"`
	Milestone *struct {
		ID     string     `json:"id"`
		Number int        `json:"number"`
		Title  string     `json:"title"`
		DueOn  *time.Time `json:"dueOn"`
		State  string     `json:"state"`
	} `json:"milestone"`
	Assignees struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
	Comments struct {
		Nodes []gqlComment `json:"nodes"`
	} `json:"comments"`

	HeadRefName       string  `json:"headRefName"`
	IsCrossRepository bool    `json:"isCrossRepository"`
	ReviewDecision    *string `json:"reviewDecision"`
	Mergeable         string  `json:"mergeable"`
	MergeStateStatus  string  `json:"mergeStateStatus"`
	Commits           struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State    string `json:"state"`
					Contexts struct {
						Nodes []struct {
							Typename   string `json:"__typename"`
							Status     string `json:"status"`
							Conclusion string `json:"conclusion"`
							State      string `json:"state"`
						} `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
				CheckSuites struct {
					Nodes []struct {
						Status     string `json:"status"`
						Conclusion string `json:"conclusion"`
					} `json:"nodes"`
				} `json:"checkSuites"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

func (n gqlNode) user() forge.User {
	if n.Author == nil {
		return forge.User{Login: "ghost"}
	}
	if n.Author.Typename == "Bot" {
		return forge.User{Login: n.Author.Login + "[bot]", Bot: true}
	}
	return forge.User{Login: n.Author.Login}
}

func (c *Client) toItem(n gqlNode) forge.Item {
	it := forge.Item{
		ID:        forge.ItemID{Repo: forge.RepoID{Host: c.host, Owner: n.Repository.Owner.Login, Name: n.Repository.Name}, Number: n.Number},
		IsPR:      n.Typename == "PullRequest",
		Title:     n.Title,
		Author:    n.user(),
		Head:      n.HeadRefName,
		UpdatedAt: n.UpdatedAt,
		URL:       n.URL,
	}
	for _, l := range n.Labels.Nodes {
		it.Labels = append(it.Labels, forge.Label{Name: l.Name, Color: l.Color})
	}
	if m := n.Milestone; m != nil {
		it.Milestone = &forge.Milestone{ID: m.ID, Number: m.Number, Title: m.Title, Due: m.DueOn, Open: m.State == "OPEN"}
	}
	for _, a := range n.Assignees.Nodes {
		it.Assignees = append(it.Assignees, forge.User{Login: a.Login})
	}
	if cs := n.Comments.Nodes; len(cs) > 0 {
		last := cs[len(cs)-1]
		it.LastCommentBy, it.LastCommentAt = last.login(), last.CreatedAt
	}
	if it.IsPR {
		checks := n.checks()
		review, mergeable := n.review(), n.mergeable()
		it.Checks, it.Review, it.Mergeable = &checks, &review, &mergeable
	}
	return it
}

var failedConclusions = []string{"FAILURE", "TIMED_OUT", "CANCELLED", "STARTUP_FAILURE", "ACTION_REQUIRED"}

func (n gqlNode) checks() forge.Checks {
	if len(n.Commits.Nodes) == 0 {
		return forge.Checks{State: forge.ChecksNone}
	}
	commit := n.Commits.Nodes[0].Commit
	awaiting := false
	for _, s := range commit.CheckSuites.Nodes {
		if s.Conclusion == "ACTION_REQUIRED" || s.Status == "WAITING" {
			awaiting = true
		}
	}
	var out forge.Checks
	if r := commit.StatusCheckRollup; r != nil {
		for _, ctx := range r.Contexts.Nodes {
			out.Total++
			if ctx.Status == "WAITING" {
				awaiting = true
			}
			if slices.Contains(failedConclusions, ctx.Conclusion) || ctx.State == "FAILURE" || ctx.State == "ERROR" {
				out.Failed++
			}
		}
		switch r.State {
		case "SUCCESS":
			out.State = forge.ChecksPassing
		case "FAILURE", "ERROR":
			out.State = forge.ChecksFailing
		default:
			out.State = forge.ChecksPending
		}
	}
	if awaiting && n.IsCrossRepository {
		out.State = forge.ChecksAwaitingApproval
	}
	return out
}

func (n gqlNode) review() forge.ReviewState {
	if n.ReviewDecision == nil {
		return forge.ReviewNone
	}
	switch *n.ReviewDecision {
	case "APPROVED":
		return forge.ReviewApproved
	case "CHANGES_REQUESTED":
		return forge.ReviewChangesRequested
	}
	return forge.ReviewNone
}

func (n gqlNode) mergeable() forge.Mergeable {
	switch {
	case n.Mergeable == "CONFLICTING":
		return forge.MergeableConflicting
	case n.MergeStateStatus == "BLOCKED":
		return forge.MergeableBlocked
	case n.Mergeable == "MERGEABLE":
		return forge.MergeableClean
	}
	return forge.MergeableUnknown
}

func withSince(q string, since *time.Time) string {
	if since == nil {
		return q
	}
	return q + " updated:>=" + since.UTC().Format(time.RFC3339)
}

// search pages through a search query, stopping at GitHub's 1000 result cap.
func (c *Client) search(ctx context.Context, q string) ([]gqlNode, bool, error) {
	var (
		nodes []gqlNode
		after any
	)
	for {
		var out struct {
			Search struct {
				IssueCount int `json:"issueCount"`
				PageInfo   struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []gqlNode `json:"nodes"`
			} `json:"search"`
		}
		if err := c.graphql(ctx, searchQuery, map[string]any{"q": q, "after": after}, &out); err != nil {
			return nil, false, err
		}
		for _, n := range out.Search.Nodes {
			if n.Typename == "Issue" || n.Typename == "PullRequest" {
				nodes = append(nodes, n)
			}
		}
		if len(nodes) >= searchCap {
			return nodes[:searchCap], out.Search.PageInfo.HasNextPage || out.Search.IssueCount > searchCap, nil
		}
		if !out.Search.PageInfo.HasNextPage {
			return nodes, false, nil
		}
		after = out.Search.PageInfo.EndCursor
	}
}

func (c *Client) searchItems(ctx context.Context, q string) (forge.ListResult[forge.Item], error) {
	nodes, trunc, err := c.search(ctx, q)
	if err != nil {
		return forge.ListResult[forge.Item]{}, err
	}
	out := forge.ListResult[forge.Item]{Truncated: trunc}
	for _, n := range nodes {
		out.Items = append(out.Items, c.toItem(n))
	}
	return out, nil
}

func (c *Client) ListMyPRs(ctx context.Context, since *time.Time) (forge.ListResult[forge.Item], error) {
	return c.searchItems(ctx, withSince("is:pr is:open author:@me archived:false sort:updated-desc", since))
}

func (c *Client) ListReviewRequests(ctx context.Context) (forge.ListResult[forge.Item], error) {
	return c.searchItems(ctx, "is:pr is:open review-requested:@me archived:false sort:updated-desc")
}

func (c *Client) ListIncoming(ctx context.Context, repos []forge.RepoID, since *time.Time) (forge.ListResult[forge.Item], error) {
	var out forge.ListResult[forge.Item]
	seen := map[forge.ItemID]bool{}
	for chunk := range slices.Chunk(repos, reposPerPage) {
		var b strings.Builder
		b.WriteString("is:open archived:false -author:@me")
		for _, r := range chunk {
			b.WriteString(" repo:" + r.String())
		}
		res, err := c.searchItems(ctx, withSince(b.String(), since))
		if err != nil {
			return forge.ListResult[forge.Item]{}, err
		}
		out.Truncated = out.Truncated || res.Truncated
		for _, it := range res.Items {
			if !seen[it.ID] {
				seen[it.ID] = true
				out.Items = append(out.Items, it)
			}
		}
	}
	return out, nil
}

func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 80 {
		return string(r[:79]) + "…"
	}
	return s
}

func (c *Client) ListMentions(ctx context.Context, since *time.Time) (forge.ListResult[forge.CommentRef], error) {
	me, err := c.WhoAmI(ctx)
	if err != nil {
		return forge.ListResult[forge.CommentRef]{}, err
	}
	nodes, trunc, err := c.search(ctx, withSince("is:open archived:false mentions:@me sort:updated-desc", since))
	if err != nil {
		return forge.ListResult[forge.CommentRef]{}, err
	}
	out := forge.ListResult[forge.CommentRef]{Truncated: trunc}
	for _, n := range nodes {
		it := c.toItem(n)
		ref := forge.CommentRef{Item: it.ID, Title: it.Title, IsPR: it.IsPR, Author: it.Author, At: it.UpdatedAt, URL: it.URL}
		for _, cm := range slices.Backward(n.Comments.Nodes) {
			if cm.login() != me.Login {
				ref.Comment, ref.Author, ref.At, ref.Excerpt = forge.CommentID(cm.ID), forge.User{Login: cm.login()}, cm.CreatedAt, excerpt(cm.BodyText)
				break
			}
		}
		out.Items = append(out.Items, ref)
	}
	return out, nil
}

const branchesQuery = `query($owner: String!, $name: String!) {
	repository(owner: $owner, name: $name) {
		defaultBranchRef { name }
		refs(refPrefix: "refs/heads/", first: 100, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}) {
			nodes {
				name
				target { ... on Commit { oid committedDate } }
				associatedPullRequests(first: 1) { totalCount }
			}
		}
	}
}`

// ListBranches returns branches pushed in the last 14 days, without the
// default branch.
func (c *Client) ListBranches(ctx context.Context, repo forge.RepoID) ([]forge.Branch, error) {
	var out struct {
		Repository *struct {
			DefaultBranchRef *struct {
				Name string `json:"name"`
			} `json:"defaultBranchRef"`
			Refs struct {
				Nodes []struct {
					Name   string `json:"name"`
					Target struct {
						OID           string    `json:"oid"`
						CommittedDate time.Time `json:"committedDate"`
					} `json:"target"`
					AssociatedPullRequests struct {
						TotalCount int `json:"totalCount"`
					} `json:"associatedPullRequests"`
				} `json:"nodes"`
			} `json:"refs"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, branchesQuery, map[string]any{"owner": repo.Owner, "name": repo.Name}, &out); err != nil {
		return nil, err
	}
	if out.Repository == nil {
		return nil, forge.ErrNotFound
	}
	def := ""
	if out.Repository.DefaultBranchRef != nil {
		def = out.Repository.DefaultBranchRef.Name
	}
	cutoff := c.now().Add(-branchWindow)
	var branches []forge.Branch
	for _, r := range out.Repository.Refs.Nodes {
		if r.Name == def || r.Target.CommittedDate.Before(cutoff) {
			continue
		}
		branches = append(branches, forge.Branch{
			Name: r.Name, Head: forge.SHA(r.Target.OID), PushedAt: r.Target.CommittedDate,
			HasPR: r.AssociatedPullRequests.TotalCount > 0,
		})
	}
	return branches, nil
}

const prQuery = `query($owner: String!, $name: String!, $number: Int!) {
	repository(owner: $owner, name: $name) {
		pullRequest(number: $number) { __typename ` + prFields + ` }
	}
}`

func (c *Client) PRChecks(ctx context.Context, pr forge.PRID) (forge.Checks, error) {
	var out struct {
		Repository *struct {
			PullRequest *gqlNode `json:"pullRequest"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": pr.Repo.Owner, "name": pr.Repo.Name, "number": pr.Number}
	if err := c.graphql(ctx, prQuery, vars, &out); err != nil {
		return forge.Checks{}, err
	}
	if out.Repository == nil || out.Repository.PullRequest == nil {
		return forge.Checks{}, forge.ErrNotFound
	}
	return out.Repository.PullRequest.checks(), nil
}
