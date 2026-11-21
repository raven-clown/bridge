package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func newRoot(out io.Writer) *cobra.Command { return newRootWith(out, wireOptions{}) }

func newRootWith(out io.Writer, opts wireOptions) *cobra.Command {
	root := &cobra.Command{
		Use:           "bridge",
		Short:         "A terminal control room for many repositories",
		SilenceUsage:  true,
		SilenceErrors: true,
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
		fmt.Fprintln(os.Stderr, "bridge:", err)
	}
	os.Exit(code)
}
