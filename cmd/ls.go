package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/ui"
)

func (a *App) newLsCmd() *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List active sshfs mounts",
		Long: "List every active sshfs mount.\n\n" +
			"A cell is left empty where it would only repeat another column, and a\n" +
			"column empty for every mount is not shown at all. A mount point appears\n" +
			"only when it is not the one smount would derive, and a status only when\n" +
			"the mount is not answering.",
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

			cfg, err := config.Load(a.home)
			if err != nil {
				return err
			}

			base := cfg.MountBaseDir()
			rows := make([][]string, 0, len(mounts))
			for _, m := range mounts {
				// A derived mount point is the mount base joined to the name in
				// the first column, so printing it repeats that column against
				// a longer path.
				mountPoint := ""
				if !m.UnderBase(base) {
					mountPoint = a.home.Collapse(m.Target)
				}
				// A healthy mount is the expected state, so saying so on every
				// row costs a column and hides the rows that need attention.
				state := ""
				if m.State != mount.StateOK {
					state = m.State.String()
				}
				rows = append(rows, []string{m.Name, mountPoint, m.Describe(), state})
			}
			return ui.RenderTable(cmd.OutOrStdout(), a.tableWidth,
				[]string{"NAME", "MOUNT POINT", "SOURCE", "STATUS"}, rows)
		},
	}
	cmd.Flags().BoolVarP(&short, "short", "s", false, "print mount names only, without the table")
	return cmd
}
