// Package cmd wires smount's command line interface. Everything it does beyond
// argument parsing and printing lives under internal.
package cmd

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/ui"
)

const rootLong = `smount mounts remote directories over SSH using sshfs.

Run it with no arguments to choose a saved favourite or an SSH host
interactively, or name a target directly:

  smount web01               mount the remote home directory
  smount web01:/var/log      mount one remote path
  smount logs                mount the favourite named "logs"

Hosts come from ~/.ssh/config and every file it includes. Favourites and
settings live in ~/.smount.`

// reservedNames returns the names a favourite must not use. A bare
// "smount <name>" resolves a subcommand before it looks for a favourite, so a
// favourite sharing a name with one could never be mounted.
//
// The set is read off the command tree rather than listed by hand, because a
// hand-written list silently goes stale: it is the aliases that get forgotten,
// and an alias resolves exactly like the name it stands for. Only top level
// commands matter, since "smount rm" is not a command even though
// "smount fav rm" is.
func reservedNames(cmd *cobra.Command) map[string]bool {
	names := make(map[string]bool)
	for _, sub := range cmd.Root().Commands() {
		names[sub.Name()] = true
		for _, alias := range sub.Aliases {
			names[alias] = true
		}
	}
	return names
}

// ErrCancelled is reported when the user backs out of a prompt.
//
// It is re-exported from ui so that the entry point can recognise a cancelled
// prompt without reaching into internal for the sentinel.
var ErrCancelled = ui.ErrCancelled

// NewRootCmd builds the command tree, writing output to out and errw.
//
// The writers are parameters rather than the process streams so that a test can
// build the same tree this binary does and read back what it wrote.
func NewRootCmd(out, errw io.Writer) *cobra.Command {
	opts := &mountOptions{}

	root := &cobra.Command{
		Use:               "smount [target]",
		Short:             "Mount remote directories over SSH with sshfs",
		Long:              rootLong,
		Args:              cobra.MaximumNArgs(1),
		SilenceErrors:     true,
		SilenceUsage:      true,
		Version:           Version,
		ValidArgsFunction: completeTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMount(cmd, opts, args)
		},
	}
	root.SetOut(out)
	root.SetErr(errw)
	opts.register(root)

	root.AddCommand(
		newLsCmd(),
		newUmountCmd(),
		newHostsCmd(),
		newFavCmd(),
		newCheckCmd(),
		newVersionCmd(),
	)
	return root
}
