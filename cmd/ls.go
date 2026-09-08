package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/ui"
)

func (a *App) newLsCmd() *cobra.Command {
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

			rows := make([][]string, 0, len(mounts))
			for _, m := range mounts {
				rows = append(rows, []string{
					m.Name,
					a.home.Collapse(m.Target),
					m.Source,
					m.State.String(),
				})
			}
			return ui.RenderTable(cmd.OutOrStdout(), a.tableWidth,
				[]string{"NAME", "MOUNT POINT", "SOURCE", "STATUS"}, rows)
		},
	}
	cmd.Flags().BoolVarP(&short, "short", "s", false, "print mount names only, without the table")
	return cmd
}
