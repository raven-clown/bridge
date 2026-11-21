// Package inbox turns forge results into one ranked list and keeps it fresh.
package inbox

import (
	"sort"
	"time"

	"github.com/raven-clown/bridge/forge"
)

type Item struct {
	Kind       forge.InboxKind    `json:"kind"`
	Repo       forge.RepoID       `json:"repo"`
	ID         forge.ItemID       `json:"id"`
	Branch     string             `json:"branch,omitempty"`
	Title      string             `json:"title"`
	Author     forge.User         `json:"author"`
	Labels     []forge.Label      `json:"labels,omitempty"`
	Milestone  *forge.Milestone   `json:"milestone,omitempty"`
	Assignees  []forge.User       `json:"assignees,omitempty"`
	Projects   []forge.ProjectRef `json:"projects,omitempty"`
	Checks     *forge.Checks      `json:"checks,omitempty"`
	Review     *forge.ReviewState `json:"review,omitempty"`
	Mergeable  *forge.Mergeable   `json:"mergeable,omitempty"`
	Unanswered int                `json:"unanswered,omitempty"`
	UpdatedAt  time.Time          `json:"updated_at"`
	SeenAt     *time.Time         `json:"seen_at,omitempty"`
	Urgency    Urgency            `json:"urgency"`
	Local      string             `json:"local,omitempty"`
	URL        string             `json:"url,omitempty"`
}

type Input struct {
	Me             string
	MyPRs          []forge.Item
	Incoming       []forge.Item
	ReviewRequests []forge.Item
	Mentions       []forge.CommentRef
	Branches       map[forge.RepoID][]forge.Branch
	Local          map[forge.RepoID]string
	Seen           func(forge.ItemID) (seenAt, answeredAt *time.Time)
	Muted          func(forge.InboxKind, forge.RepoID, forge.User) bool
}

func fromForge(kind forge.InboxKind, f forge.Item) Item {
	return Item{
		Kind: kind, Repo: f.ID.Repo, ID: f.ID, Branch: f.Head, Title: f.Title, Author: f.Author,
		Labels: f.Labels, Milestone: f.Milestone, Assignees: f.Assignees, Projects: f.Projects,
		Checks: f.Checks, Review: f.Review, Mergeable: f.Mergeable, UpdatedAt: f.UpdatedAt, URL: f.URL,
	}
}

func after(t time.Time, mark *time.Time) bool { return mark == nil || t.After(*mark) }

func Build(in Input) []Item {
	var order []forge.ItemID
	byID := map[forge.ItemID]*Item{}
	last := map[forge.ItemID]forge.Item{}
	add := func(kind forge.InboxKind, f forge.Item) {
		if _, ok := byID[f.ID]; ok {
			return
		}
		it := fromForge(kind, f)
		byID[f.ID] = &it
		last[f.ID] = f
		order = append(order, f.ID)
	}

	for _, f := range in.MyPRs {
		add(forge.OwnPR, f)
	}
	for _, f := range in.ReviewRequests {
		add(forge.ReviewRequest, f)
	}
	for _, f := range in.Incoming {
		kind := forge.IncomingIssue
		if f.IsPR {
			kind = forge.IncomingPR
		}
		add(kind, f)
	}

	mentions := map[forge.ItemID]map[forge.CommentID]time.Time{}
	for _, m := range in.Mentions {
		if _, ok := byID[m.Item]; !ok {
			add(forge.Mention, forge.Item{ID: m.Item, IsPR: m.IsPR, Title: m.Title, Author: m.Author, UpdatedAt: m.At, URL: m.URL})
		}
		if mentions[m.Item] == nil {
			mentions[m.Item] = map[forge.CommentID]time.Time{}
		}
		mentions[m.Item][m.Comment] = m.At
	}

	var out []Item
	for _, key := range order {
		it := byID[key]
		seenAt, answeredAt := in.Seen(key)
		it.SeenAt = seenAt
		for _, at := range mentions[key] {
			if after(at, answeredAt) {
				it.Unanswered++
			}
		}
		if f := last[key]; it.Unanswered == 0 && f.LastCommentBy != "" && f.LastCommentBy != in.Me && after(f.LastCommentAt, answeredAt) {
			it.Unanswered = 1
		}
		if it.Kind == forge.Mention && it.Unanswered == 0 {
			continue
		}
		out = append(out, *it)
	}

	for repo, branches := range in.Branches {
		for _, b := range branches {
			if b.HasPR {
				continue
			}
			out = append(out, Item{Kind: forge.PushedBranch, Repo: repo, ID: forge.ItemID{Repo: repo}, Branch: b.Name, Title: b.Name, UpdatedAt: b.PushedAt})
		}
	}

	kept := out[:0]
	for _, it := range out {
		if in.Muted(it.Kind, it.Repo, it.Author) {
			continue
		}
		it.Urgency = Rank(it)
		it.Local = in.Local[it.Repo]
		kept = append(kept, it)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].Urgency != kept[j].Urgency {
			return kept[i].Urgency > kept[j].Urgency
		}
		return kept[i].UpdatedAt.Before(kept[j].UpdatedAt)
	})
	return kept
}
