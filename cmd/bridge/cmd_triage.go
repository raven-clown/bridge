package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/triage"
)

func newTriageCmd(opts *wireOptions) *cobra.Command {
	var (
		labels, unlabels, assign, unassign []string
		milestone, project                 string
		noMilestone, closeIt, reopen       bool
	)
	cmd := &cobra.Command{
		Use:   "triage <ref>...",
		Short: "Label, assign, set a milestone, move on a project, close or reopen",
		Long: "Apply one change to every reference given. A reference is an issue or pull request URL,\n" +
			"owner/name#number or host/owner/name#number. --project takes \"Project title/Status column\".",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if closeIt && reopen {
				return errors.New("--close and --reopen cannot be used together")
			}
			ch := triage.Change{AddLabels: labels, RemoveLabels: unlabels, AddAssignees: assign, RemoveAssignees: unassign, ClearMilestone: noMilestone}
			if milestone != "" {
				ch.Milestone = &milestone
			}
			if closeIt || reopen {
				s := forge.StateOpen
				if closeIt {
					s = forge.StateClosed
				}
				ch.State = &s
			}
			var ids []forge.ItemID
			for _, a := range args {
				id, err := parseRef(a)
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}
			if project == "" && isEmpty(ch) {
				return errors.New("nothing to change: pass --label, --unlabel, --milestone, --no-milestone, --assign, --unassign, --project, --close or --reopen")
			}

			a, err := wireCmd(cmd, *opts)
			if err != nil {
				return err
			}
			// GitHub ignores an unknown assignee, so "me" must become a login.
			ch.AddAssignees, ch.RemoveAssignees = resolveMe(ch.AddAssignees, a.me), resolveMe(ch.RemoveAssignees, a.me)
			if project != "" {
				move, err := resolveProject(cmd.Context(), a.forge, ids[0].Repo.Owner, project)
				if err != nil {
					return err
				}
				ch.Project = move
			}
			failed := 0
			for _, r := range triage.Apply(cmd.Context(), a.forge, ids, ch) {
				if r.Err != nil {
					failed++
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "fail %s: %s\n", r.Item, termsafe.Line(r.Err.Error()))
					continue
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "ok %s\n", r.Item)
			}
			if failed > 0 {
				return &exitError{code: 1, err: fmt.Errorf("%d of %d failed", failed, len(ids))}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&labels, "label", nil, "add a label (repeatable)")
	f.StringSliceVar(&unlabels, "unlabel", nil, "remove a label (repeatable)")
	f.StringVar(&milestone, "milestone", "", "set the milestone by title")
	f.BoolVar(&noMilestone, "no-milestone", false, "clear the milestone")
	f.StringSliceVar(&assign, "assign", nil, "add an assignee (repeatable)")
	f.StringSliceVar(&unassign, "unassign", nil, "remove an assignee (repeatable)")
	f.StringVar(&project, "project", "", `add to a project and set its Status column: "Title/Column"`)
	f.BoolVar(&closeIt, "close", false, "close")
	f.BoolVar(&reopen, "reopen", false, "reopen")
	return cmd
}

func resolveMe(logins []string, me string) []string {
	out := make([]string, 0, len(logins))
	for _, l := range logins {
		if strings.EqualFold(l, "me") || l == "@me" {
			l = me
		}
		out = append(out, l)
	}
	return out
}

func isEmpty(ch triage.Change) bool {
	return len(ch.AddLabels) == 0 && len(ch.RemoveLabels) == 0 && ch.Milestone == nil && !ch.ClearMilestone &&
		len(ch.AddAssignees) == 0 && len(ch.RemoveAssignees) == 0 && ch.State == nil && ch.Project == nil
}

// resolveProject turns "Title/Column" into ids, using the owner's projects
// and their single select field named Status. The column is optional.
func resolveProject(ctx context.Context, f forge.Forge, owner, spec string) (*triage.ProjectMove, error) {
	title, column := spec, ""
	if i := strings.LastIndex(spec, "/"); i >= 0 {
		title, column = spec[:i], spec[i+1:]
	}
	projects, err := f.ListProjects(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("projects: %w", err)
	}
	for _, p := range projects {
		if !strings.EqualFold(p.Title, title) {
			continue
		}
		move := &triage.ProjectMove{ProjectID: p.ID}
		if column == "" {
			return move, nil
		}
		for _, fld := range p.Fields {
			if !strings.EqualFold(fld.Name, "Status") {
				continue
			}
			for _, o := range fld.Options {
				if strings.EqualFold(o.Name, column) {
					move.FieldID, move.OptionID = fld.ID, o.ID
					return move, nil
				}
			}
		}
		return nil, fmt.Errorf("project %q has no Status column %q", p.Title, column)
	}
	return nil, fmt.Errorf("no project %q for %s", title, owner)
}
