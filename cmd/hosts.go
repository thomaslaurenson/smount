package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/sshconf"
)

func newHostsCmd() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "hosts",
		Short: "List the SSH hosts smount can mount",
		Long: "List every host alias in the ssh config and the files it includes.\n" +
			"Wildcard patterns such as \"Host *\" are omitted, since they configure\n" +
			"connections rather than name somewhere to connect to.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			aliases, err := sshconf.Aliases(cfg.SSHConfigPath())
			if err != nil {
				return err
			}

			if quiet {
				for _, alias := range aliases {
					fmt.Fprintln(cmd.OutOrStdout(), alias)
				}
				return nil
			}

			resolved := sshconf.ResolveAll(aliases)
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "HOST\tRESOLVES TO\tIDENTITY")
			for _, alias := range aliases {
				host := resolved[alias]
				if host == nil {
					fmt.Fprintf(w, "%s\t-\t-\n", alias)
					continue
				}
				identity := host.Identity
				if identity == "" {
					identity = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", alias, host.Addr(), identity)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "print host names only, without resolving them")
	return cmd
}
