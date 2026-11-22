package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/review"
)

func newReviewCmd(opts *wireOptions) *cobra.Command {
	var (
		asJSON, approve, requestChanges, comment, force bool
		message                                         string
	)
	cmd := &cobra.Command{
		Use:   "review <url | owner/name#number>",
		Short: "List a pull request's files, or submit a review",
		Long: "With no flag, list the changed files. With --approve, --request-changes or --comment, submit a review;\n" +
			"comments drafted in the terminal UI are included. -m sets the summary, which a comment or a\n" +
			"request for changes needs.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseRef(args[0])
			if err != nil {
				return err
			}
			var event forge.ReviewEvent
			n := 0
			for _, c := range []struct {
				on bool
				ev forge.ReviewEvent
			}{{approve, forge.EventApprove}, {requestChanges, forge.EventRequestChanges}, {comment, forge.EventComment}} {
				if c.on {
					n++
					event = c.ev
				}
			}
			if n > 1 {
				return errors.New("use one of --approve, --request-changes and --comment")
			}

			a, err := wireCmd(cmd, *opts)
			if err != nil {
				return err
			}
			if n == 0 {
				d, err := a.forge.PRDiff(cmd.Context(), id)
				if err != nil {
					return err
				}
				if asJSON {
					return writeJSON(cmd.OutOrStdout(), d)
				}
				return printFiles(cmd.OutOrStdout(), d)
			}

			draft, ok, err := review.LoadDraft(a.store, id)
			if err != nil {
				return err
			}
			if !ok {
				d, err := a.forge.PRDiff(cmd.Context(), id)
				if err != nil {
					return err
				}
				draft = review.Draft{PR: id, HeadSHA: d.HeadSHA}
			}
			if message != "" {
				draft.Summary = message
			}
			err = review.Submit(cmd.Context(), a.forge, a.store, draft, event, force)
			switch {
			case errors.Is(err, review.ErrNeedsSummary):
				return fmt.Errorf("%w: pass -m \"...\"", err)
			case errors.Is(err, forge.ErrStaleHead):
				return &exitError{code: 1, err: errors.New("the pull request changed since the draft was written; run again with --force to submit against the new head")}
			case err != nil:
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "submitted %s on %s with %d comments\n", event, id, len(draft.Comments))
			return err
		},
	}
	f := cmd.Flags()
	f.BoolVar(&asJSON, "json", false, "print the diff as JSON")
	f.BoolVar(&approve, "approve", false, "approve")
	f.BoolVar(&requestChanges, "request-changes", false, "request changes (needs -m)")
	f.BoolVar(&comment, "comment", false, "comment without approving (needs -m)")
	f.StringVarP(&message, "message", "m", "", "review summary")
	f.BoolVar(&force, "force", false, "submit even if the pull request changed since the draft")
	return cmd
}

func printFiles(w io.Writer, d forge.Diff) error {
	adds, dels := 0, 0
	for _, p := range d.Files {
		f, err := review.Parse(p)
		if err != nil {
			f = review.File{Patch: p, Missing: true}
		}
		review.Collapse(&f)
		line := fmt.Sprintf("%-8s %s +%d -%d", p.Status, termsafe.Line(p.Path), p.Additions, p.Deletions)
		if f.Reason != "" {
			line += " (" + f.Reason + ")"
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
		adds, dels = adds+p.Additions, dels+p.Deletions
	}
	if d.Truncated {
		if _, err := fmt.Fprintln(w, "GitHub lists at most 3000 files; the rest are not shown"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "%d files, +%d -%d\n", len(d.Files), adds, dels)
	return err
}
