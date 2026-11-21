package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func newRoot(out io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "bridge",
		Short:         "A terminal control room for many repositories",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "bridge %s\n", version)
			return err
		},
	})
	return root
}

func main() {
	if err := newRoot(os.Stdout).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "bridge:", err)
		os.Exit(1)
	}
}
