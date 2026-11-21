package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/triage"
)

func newIssueCmd(opts *wireOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "issue", Short: "Work with issues"}
	var (
		d        triage.IssueDraft
		bodyFile string
	)
	newCmd := &cobra.Command{
		Use:   "new <owner/name>",
		Short: "Create an issue, filling labels, milestone, assignee and project from your policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := forge.ParseRepoID(args[0])
			if err != nil {
				return err
			}
			if bodyFile != "" {
				b, err := os.ReadFile(bodyFile) //nolint:gosec // the user names the file
				if err != nil {
					return err
				}
				d.Body = string(b)
			}
			a, err := wire(cmd.Context(), *opts)
			if err != nil {
				return err
			}
			id, err := triage.CreateIssue(cmd.Context(), a.forge, a.cfg, repo, d)
			if id.Number != 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), issueURL(id))
			}
			if err != nil && id.Number != 0 {
				return &exitError{code: 1, err: err}
			}
			return err
		},
	}
	f := newCmd.Flags()
	f.StringVar(&d.Title, "title", "", "title (required)")
	f.StringVar(&d.Body, "body", "", "body")
	f.StringVar(&bodyFile, "body-file", "", "read the body from a file")
	f.StringSliceVar(&d.Labels, "label", nil, "label (repeatable); default from policy sidebar.labels")
	f.StringSliceVar(&d.Assignees, "assign", nil, "assignee (repeatable); default from policy sidebar.assignee")
	f.StringVar(&d.Milestone, "milestone", "", `milestone title, "current" or "none"; default from policy`)
	f.StringVar(&d.Project, "project", "", "project title; default from policy")
	newCmd.PreRunE = func(*cobra.Command, []string) error {
		if d.Title == "" {
			return errors.New("--title is required")
		}
		if d.Body != "" && bodyFile != "" {
			return errors.New("use --body or --body-file, not both")
		}
		return nil
	}
	cmd.AddCommand(newCmd)
	return cmd
}
