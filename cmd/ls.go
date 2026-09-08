package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/tilde"
)

func newLsCmd(home tilde.Home) *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:     "ls",
		Short:   "List active sshfs mounts",
		Args:    cobra.NoArgs,
		Aliases: []string{"status"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			mounts, err := mount.Active(cmd.Context())
			if err != nil {
				return err
			}
			if len(mounts) == 0 {
				// Noted on stderr, so that a run with nothing mounted leaves
				// stdout empty rather than leaving a consumer a line to parse.
				fmt.Fprintln(cmd.ErrOrStderr(), "[*] no active sshfs mounts")
				return nil
			}

			if short {
				for _, m := range mounts {
					fmt.Fprintln(cmd.OutOrStdout(), m.Name)
				}
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
	cmd.Flags().BoolVarP(&short, "short", "s", false, "print mount names only, without the table")
	return cmd
}
