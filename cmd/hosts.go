package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/sshconf"
	"github.com/thomaslaurenson/smount/internal/ui"
)

func (a *App) newHostsCmd() *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "hosts",
		Short: "List the SSH hosts smount can mount",
		Long: "List every host alias in the ssh config and the files it includes.\n" +
			"Wildcard patterns such as \"Host *\" are omitted, since they configure\n" +
			"connections rather than name somewhere to connect to.\n\n" +
			"The second column is empty for a host that resolves to itself as the\n" +
			"local user on the default port, since there it would only repeat the\n" +
			"first column.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			aliases, err := sshconf.Aliases(string(a.home), cfg.SSHConfigPath())
			if err != nil {
				return err
			}
			if len(aliases) == 0 {
				// Noted on stderr, so that a config naming no host leaves
				// stdout empty rather than leaving a consumer a header to
				// parse. The file is named because an empty listing usually
				// means smount is reading a different one than expected.
				a.ui.Infof("no hosts in %s", a.home.Collapse(cfg.SSHConfigPath()))
				return nil
			}

			if short {
				for _, alias := range aliases {
					fmt.Fprintln(cmd.OutOrStdout(), alias)
				}
				return nil
			}

			// A baseline that will not resolve is not worth failing the listing
			// over. A nil one simply leaves every setting in.
			resolved, defaults := sshconf.ResolveAll(cmd.Context(), aliases)

			rows := make([][]string, 0, len(aliases))
			for _, alias := range aliases {
				host := resolved[alias]
				if host == nil {
					rows = append(rows, []string{alias, "-"})
					continue
				}
				rows = append(rows, []string{alias, host.Describe(defaults)})
			}
			return ui.RenderTable(cmd.OutOrStdout(), a.tableWidth,
				[]string{"HOST", "RESOLVES TO"}, rows)
		},
	}
	cmd.Flags().BoolVarP(&short, "short", "s", false, "print host names only, without resolving them")
	return cmd
}
