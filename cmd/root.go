// Package cmd wires smount's command line interface. Everything it does beyond
// argument parsing and printing lives under internal.
package cmd

import (
	"errors"

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

// Execute runs the command line interface.
//
// A cancelled prompt is not a failure. Backing out of a menu is a normal way to
// finish, so it is reported and exits zero rather than being passed up as an
// error the entry point would print and exit one for.
func Execute() error {
	err := newRootCmd().Execute()
	if errors.Is(err, ui.ErrCancelled) {
		ui.Infof("cancelled")
		return nil
	}
	return err
}

func newRootCmd() *cobra.Command {
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
	opts.register(root)

	root.AddCommand(
		newLsCmd(),
		newUmountCmd(),
		newHostsCmd(),
		newFavCmd(),
		newCheckCmd(),
		versionCmd,
	)
	return root
}
