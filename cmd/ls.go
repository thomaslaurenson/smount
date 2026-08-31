package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/tilde"
)

func newLsCmd(home tilde.Home) *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Short:   "List active sshfs mounts",
		Args:    cobra.NoArgs,
		Aliases: []string{"status"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			mounts, err := mount.Active()
			if err != nil {
				return err
			}
			if len(mounts) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "[*] no active sshfs mounts")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tMOUNT POINT\tSOURCE\tSTATUS")
			for _, m := range mounts {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", m.Name, home.Collapse(m.Target), m.Source, m.State)
			}
			return w.Flush()
		},
	}
}
