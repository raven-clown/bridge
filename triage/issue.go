package triage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/forge"
)

var (
	ErrNoTitle        = errors.New("an issue needs a title")
	ErrUnknownProject = errors.New("unknown project")
)

// IssueDraft names its milestone; empty fields take the repository's
// sidebar defaults.
type IssueDraft struct {
	Title     string
	Body      string
	Labels    []string
	Assignees []string
	Milestone string
	Project   string
}

// current is the open milestone with the nearest due date.
func current(ms []forge.Milestone) *forge.MilestoneID {
	var best *forge.Milestone
	for i := range ms {
		m := &ms[i]
		if !m.Open || m.Due == nil {
			continue
		}
		if best == nil || m.Due.Before(*best.Due) {
			best = m
		}
	}
	if best == nil {
		return nil
	}
	n := forge.MilestoneID(best.Number)
	return &n
}

// CreateIssue returns the new issue's id even when adding it to the
// project fails afterwards, so the caller can still show it.
func CreateIssue(ctx context.Context, f forge.Forge, cfg config.Config, repo forge.RepoID, d IssueDraft) (forge.ItemID, error) {
	if strings.TrimSpace(d.Title) == "" {
		return forge.ItemID{}, ErrNoTitle
	}
	side := cfg.PolicyFor(repo).Sidebar
	issue := forge.NewIssue{Title: d.Title, Body: d.Body, Labels: d.Labels, Assignees: d.Assignees}
	if len(issue.Labels) == 0 {
		issue.Labels = side.Labels
	}
	if len(issue.Assignees) == 0 {
		switch side.Assignee {
		case "", "none":
		case "me":
			me, err := f.WhoAmI(ctx)
			if err != nil {
				return forge.ItemID{}, err
			}
			issue.Assignees = []string{me.Login}
		default:
			issue.Assignees = []string{side.Assignee}
		}
	}
	milestone := d.Milestone
	if milestone == "" {
		milestone = side.Milestone
	}
	if milestone != "" && milestone != "none" {
		meta, err := f.RepoMeta(ctx, repo)
		if err != nil {
			return forge.ItemID{}, fmt.Errorf("milestone: %w", err)
		}
		if milestone == "current" {
			issue.Milestone = current(meta.Milestones)
		} else {
			n, err := findMilestone(meta.Milestones, milestone)
			if err != nil {
				return forge.ItemID{}, err
			}
			issue.Milestone = &n
		}
	}

	id, err := f.CreateIssue(ctx, repo, issue)
	if err != nil {
		return forge.ItemID{}, err
	}
	project := d.Project
	if project == "" {
		project = side.Project
	}
	if project == "" {
		return id, nil
	}
	return id, addToProjectByTitle(ctx, f, repo.Owner, project, id)
}

func addToProjectByTitle(ctx context.Context, f forge.Forge, owner, title string, id forge.ItemID) error {
	projects, err := f.ListProjects(ctx, owner)
	if err != nil {
		return fmt.Errorf("created %s, but listing projects failed: %w", id, err)
	}
	for _, p := range projects {
		if strings.EqualFold(p.Title, title) {
			if _, err := f.AddToProject(ctx, p.ID, id); err != nil {
				return fmt.Errorf("created %s, but adding it to %s failed: %w", id, title, err)
			}
			return nil
		}
	}
	return fmt.Errorf("created %s, but %w %q", id, ErrUnknownProject, title)
}
