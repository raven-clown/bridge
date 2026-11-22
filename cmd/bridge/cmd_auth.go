package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/raven-clown/bridge/auth"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/github"
)

type printPrompt struct{ w io.Writer }

func (p printPrompt) ShowCode(url, code string) {
	_, _ = fmt.Fprintf(p.w, "Open %s and enter the code %s\n", url, code)
}

func tokenSource(env func(string) string) string {
	for _, name := range []string{"BRIDGE_TOKEN", "GITHUB_TOKEN"} {
		if env(name) != "" {
			return name
		}
	}
	return "keyring"
}

func newAuthCmd(opts *wireOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Sign in and check the sign-in"}

	var host string
	login := &cobra.Command{
		Use:   "login",
		Short: "Sign in with the GitHub device flow and keep the token in the OS keyring",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			df := &auth.DeviceFlow{ClientID: auth.ClientID, Host: host, Prompt: printPrompt{cmd.OutOrStdout()}}
			tok, err := df.Run(cmd.Context(), strings.Fields(auth.Scopes))
			if err != nil {
				return err
			}
			if err := auth.OSKeyring().Set(auth.Service, host, tok); err != nil {
				return fmt.Errorf("signed in, but saving the token to the keyring failed: %s", strings.ReplaceAll(err.Error(), tok, "***"))
			}
			c, err := github.New(host, tok, nil)
			if err != nil {
				return err
			}
			me, err := c.WhoAmI(cmd.Context())
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "signed in to %s as %s\n", host, me.Login)
			return err
		},
	}
	login.Flags().StringVar(&host, "host", forge.DefaultHost, "GitHub host")

	status := &cobra.Command{
		Use:   "status",
		Short: "Show who you are signed in as and where the token comes from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := wire(cmd.Context(), *opts)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "signed in to %s as %s (token from %s)\n", forge.DefaultHost, a.me, tokenSource(a.env))
			return err
		},
	}
	cmd.AddCommand(login, status)
	return cmd
}
