package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/thread"
)

func newThreadCmd(opts *wireOptions) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "thread <url | owner/name#number>",
		Short: "Show an issue or pull request with its comments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseRef(args[0])
			if err != nil {
				return err
			}
			a, err := wire(cmd.Context(), *opts)
			if err != nil {
				return err
			}
			th, err := thread.Load(cmd.Context(), a.forge, a.store, id)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), th)
			}
			return printThread(cmd.OutOrStdout(), a, th)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func names[T any](xs []T, name func(T) string) string {
	var out []string
	for _, x := range xs {
		out = append(out, name(x))
	}
	return strings.Join(out, ", ")
}

func printThread(w io.Writer, a *app, th forge.Thread) error {
	it := th.Item
	line := termsafe.Line
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", it.ID, line(it.Title))
	meta := []string{"by " + line(it.Author.Login)}
	if len(it.Labels) > 0 {
		meta = append(meta, "labels: "+line(names(it.Labels, func(l forge.Label) string { return l.Name })))
	}
	if it.Milestone != nil {
		meta = append(meta, "milestone: "+line(it.Milestone.Title))
	}
	if len(it.Assignees) > 0 {
		meta = append(meta, "assignees: "+line(names(it.Assignees, func(u forge.User) string { return u.Login })))
	}
	fmt.Fprintf(&b, "%s\n", strings.Join(meta, " · "))
	if it.URL != "" {
		fmt.Fprintf(&b, "%s\n", it.URL)
	}
	if body := strings.TrimSpace(th.Body); body != "" {
		fmt.Fprintf(&b, "\n%s\n", body)
	}
	if th.HiddenComments > 0 {
		fmt.Fprintf(&b, "\n(%d earlier comments are not shown)\n", th.HiddenComments)
	}
	for _, c := range th.Comments {
		who := line(c.Author.Login)
		if c.Review != "" {
			who += ", " + strings.ReplaceAll(line(c.Review), "_", " ")
		}
		fmt.Fprintf(&b, "\n--- %s, %s ago\n%s\n", who, age(a.now(), c.At), strings.TrimSpace(c.Body))
	}
	for _, rt := range th.ReviewThreads {
		state := ""
		if rt.Resolved {
			state = " (resolved)"
		}
		fmt.Fprintf(&b, "\n=== %s:%d%s\n", line(rt.Path), rt.Line, state)
		for _, c := range rt.Comments {
			fmt.Fprintf(&b, "--- %s, %s ago\n%s\n", line(c.Author.Login), age(a.now(), c.At), strings.TrimSpace(c.Body))
		}
	}
	_, err := io.WriteString(w, termsafe.Text(b.String()))
	return err
}
