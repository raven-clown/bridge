package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/internal/tui"
)

var version = "dev"

func newRoot(out io.Writer) *cobra.Command { return newRootWith(out, wireOptions{}) }

func newRootWith(out io.Writer, opts wireOptions) *cobra.Command {
	root := &cobra.Command{
		Use:           "bridge",
		Short:         "A terminal control room for many repositories",
		Long:          "Run with no command to open the terminal UI.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := wire(cmd.Context(), opts)
			if err != nil {
				return err
			}
			d := tui.Deps{Refresher: a.refresher(), Forge: a.forge, Store: a.store, Config: a.cfg, Me: a.me, Now: a.now}
			if len(a.warnings) > 0 {
				d.Warning = a.warnings[0]
			}
			return tui.Run(cmd.Context(), d)
		},
	}
	root.SetOut(out)
	root.PersistentFlags().StringVar(&opts.configPath, "config", opts.configPath, "config file (default: the platform config directory)")
	root.PersistentFlags().BoolVar(&opts.rescan, "rescan", false, "rescan the roots for repositories")

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "bridge %s\n", version)
			return err
		},
	})
	root.AddCommand(
		newInboxCmd(&opts),
		newThreadCmd(&opts),
		newTriageCmd(&opts),
		newIssueCmd(&opts),
		newAuthCmd(&opts),
	)
	return root
}

func main() {
	err := newRoot(os.Stdout).Execute()
	if err == nil {
		return
	}
	code := 1
	var ee *exitError
	if errors.As(err, &ee) {
		code = ee.code
		err = ee.err
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, errorText(err))
	}
	os.Exit(code)
}

// errorText escapes the error, since GitHub's messages can echo text that
// anyone who opens an issue controls.
func errorText(err error) string { return "bridge: " + termsafe.Line(err.Error()) }
