// Package triage applies labels, milestones, assignees, state and project
// moves to one or many items.
package triage

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/raven-clown/bridge/forge"
)

var ErrUnknownMilestone = errors.New("unknown milestone")

type Change struct {
	AddLabels       []string
	RemoveLabels    []string
	Milestone       *string
	ClearMilestone  bool
	AddAssignees    []string
	RemoveAssignees []string
	State           *forge.ItemState
	Project         *ProjectMove
}

type ProjectMove struct {
	ProjectID forge.ProjectID
	FieldID   forge.FieldID
	OptionID  string
}

type Result struct {
	Item forge.ItemID
	Err  error
}

func findMilestone(ms []forge.Milestone, name string) (forge.MilestoneID, error) {
	for _, m := range ms {
		if m.Title == name {
			return forge.MilestoneID(m.Number), nil
		}
	}
	return 0, fmt.Errorf("%w %q", ErrUnknownMilestone, name)
}

// Apply changes each item on its own; one failure does not stop the rest.
// Results come back in input order.
func Apply(ctx context.Context, f forge.Forge, items []forge.ItemID, ch Change) []Result {
	var (
		mu    sync.Mutex
		metas = map[forge.RepoID]forge.RepoMeta{}
	)
	meta := func(repo forge.RepoID) (forge.RepoMeta, error) {
		mu.Lock()
		defer mu.Unlock()
		if m, ok := metas[repo]; ok {
			return m, nil
		}
		m, err := f.RepoMeta(ctx, repo)
		if err != nil {
			return forge.RepoMeta{}, err
		}
		metas[repo] = m
		return m, nil
	}

	results := make([]Result, len(items))
	var g errgroup.Group
	g.SetLimit(4)
	for i, id := range items {
		g.Go(func() error {
			results[i] = Result{Item: id, Err: applyOne(ctx, f, id, ch, meta)}
			return nil
		})
	}
	_ = g.Wait()
	return results
}

func applyOne(ctx context.Context, f forge.Forge, id forge.ItemID, ch Change, meta func(forge.RepoID) (forge.RepoMeta, error)) error {
	var milestone *forge.MilestoneID
	if ch.Milestone != nil {
		m, err := meta(id.Repo)
		if err != nil {
			return fmt.Errorf("milestone: %w", err)
		}
		n, err := findMilestone(m.Milestones, *ch.Milestone)
		if err != nil {
			return err
		}
		milestone = &n
	}
	if len(ch.AddLabels) > 0 || len(ch.RemoveLabels) > 0 {
		if err := f.SetLabels(ctx, id, ch.AddLabels, ch.RemoveLabels); err != nil {
			return fmt.Errorf("labels: %w", err)
		}
	}
	if milestone != nil || ch.ClearMilestone {
		if err := f.SetMilestone(ctx, id, milestone); err != nil {
			return fmt.Errorf("milestone: %w", err)
		}
	}
	if len(ch.AddAssignees) > 0 || len(ch.RemoveAssignees) > 0 {
		if err := f.SetAssignees(ctx, id, ch.AddAssignees, ch.RemoveAssignees); err != nil {
			return fmt.Errorf("assignees: %w", err)
		}
	}
	if ch.State != nil {
		if err := f.SetState(ctx, id, *ch.State); err != nil {
			return fmt.Errorf("state: %w", err)
		}
	}
	if p := ch.Project; p != nil {
		pi, err := f.AddToProject(ctx, p.ProjectID, id)
		if err != nil {
			return fmt.Errorf("project: %w", err)
		}
		if p.FieldID != "" {
			if err := f.SetProjectField(ctx, p.ProjectID, pi, p.FieldID, forge.FieldValue{OptionID: p.OptionID}); err != nil {
				return fmt.Errorf("project: %w", err)
			}
		}
	}
	return nil
}
