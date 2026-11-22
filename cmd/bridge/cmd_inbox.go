package main

import (
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/inbox"
	"github.com/raven-clown/bridge/internal/termsafe"
)

func newInboxCmd(opts *wireOptions) *cobra.Command {
	var (
		asJSON bool
		kind   string
		repo   string
	)
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "List everything waiting for you, most urgent first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var wantKind *forge.InboxKind
			if kind != "" {
				k, ok := forge.ParseInboxKind(kind)
				if !ok {
					return fmt.Errorf("unknown kind %q", kind)
				}
				wantKind = &k
			}
			var wantRepo *forge.RepoID
			if repo != "" {
				r, err := forge.ParseRepoID(repo)
				if err != nil {
					return err
				}
				wantRepo = &r
			}

			a, err := wireCmd(cmd, *opts)
			if err != nil {
				return err
			}
			res := a.refresher().Refresh(cmd.Context())
			kept := res.Items[:0]
			for _, it := range res.Items {
				if (wantKind == nil || it.Kind == *wantKind) && (wantRepo == nil || it.Repo == *wantRepo) {
					kept = append(kept, it)
				}
			}
			res.Items = kept

			warn(cmd, res)
			if asJSON {
				if err := writeJSON(cmd.OutOrStdout(), res); err != nil {
					return err
				}
			} else if err := printInbox(cmd, a, res); err != nil {
				return err
			}
			if len(res.Items) == 0 && len(res.Problems) > 0 {
				return &exitError{code: 2, err: errors.New("every source failed")}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&kind, "kind", "", "only this kind: own-pr, incoming-pr, incoming-issue, review-request, mention, pushed-branch")
	cmd.Flags().StringVar(&repo, "repo", "", "only this repository (owner/name)")
	return cmd
}

func warn(cmd *cobra.Command, res inbox.Result) {
	w := cmd.ErrOrStderr()
	for _, p := range res.Problems {
		where := p.Source
		if p.Repo != nil {
			where += " " + p.Repo.String()
		}
		line := fmt.Sprintf("warning: %s: %v", where, p.Err)
		if p.RetryAt != nil {
			line += fmt.Sprintf(" (retry after %s)", p.RetryAt.Local().Format("15:04"))
		}
		_, _ = fmt.Fprintln(w, termsafe.Line(line))
	}
	for _, t := range res.Truncated {
		_, _ = fmt.Fprintf(w, "warning: %s has more than 1000 results; showing the first 1000\n", t)
	}
}

func printInbox(cmd *cobra.Command, a *app, res inbox.Result) error {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "\tREPO\tKIND\tREF\tTITLE\tAUTHOR\tCHECKS\tREPLY\tAGE")
	for _, it := range res.Items {
		reply := ""
		if it.Unanswered > 0 {
			reply = fmt.Sprintf("↩%d", it.Unanswered)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			urgencyGlyph(it.Urgency), termsafe.Line(it.Repo.String()), it.Kind, termsafe.Line(itemRef(it)),
			truncate(termsafe.Line(it.Title), 50), termsafe.Line(it.Author.Login),
			checksGlyph(it.Checks), reply, age(a.now(), it.UpdatedAt))
	}
	return tw.Flush()
}
