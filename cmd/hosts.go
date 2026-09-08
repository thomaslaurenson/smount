package cmd

import (
	"fmt"
	"os/user"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/config"
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
			cfg, err := config.Load(a.home)
			if err != nil {
				return err
			}
			aliases, err := sshconf.Aliases(string(a.home), cfg.SSHConfigPath())
			if err != nil {
				return err
			}

			if short {
				for _, alias := range aliases {
					fmt.Fprintln(cmd.OutOrStdout(), alias)
				}
				return nil
			}

			resolved := sshconf.ResolveAll(cmd.Context(), aliases)
			rows := make([][]string, 0, len(aliases))
			for _, alias := range aliases {
				host := resolved[alias]
				if host == nil {
					rows = append(rows, []string{alias, "-"})
					continue
				}
				rows = append(rows, []string{alias, host.Describe(localUser())})
			}
			return ui.RenderTable(cmd.OutOrStdout(), a.tableWidth,
				[]string{"HOST", "RESOLVES TO"}, rows)
		},
	}
	cmd.Flags().BoolVarP(&short, "short", "s", false, "print host names only, without resolving them")
	return cmd
}

// localUser returns the name of the user running smount, or the empty string
// when it cannot be determined.
//
// It decides which resolved users are worth printing: ssh reports a user for
// every host, and on most of them it is simply this one, which the alias
// already implies. An empty answer prints every user, which is the right way
// to fail here.
func localUser() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.Username
}
