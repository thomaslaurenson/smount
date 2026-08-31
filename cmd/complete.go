package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/favourites"
	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/sshconf"
)

// Every completer here reports ShellCompDirectiveNoFileComp rather than
// ShellCompDirectiveError when it cannot read what it needs.
//
// The error directive makes the shell fall back to filename completion, so an
// unreadable favourites file would answer "smount umount <TAB>" with the
// contents of the current directory. None of these arguments is ever a local
// path, so no suggestion is the honest answer and a wrong one is worse than
// none.

// completeTargets offers favourite names and ssh host aliases for the bare
// target argument.
//
// Hosts are offered unresolved. Completion runs on every tab press, and eighty
// ssh processes is too much work to do while somebody is waiting to finish
// typing a word.
func (a *App) completeTargets(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var out []string
	if store, err := favourites.Load(a.home); err == nil {
		out = append(out, store.Names()...)
	}
	if cfg, err := config.Load(a.home); err == nil {
		if aliases, err := sshconf.Aliases(string(a.home), cfg.SSHConfigPath()); err == nil {
			out = append(out, aliases...)
		}
	}
	return filterByPrefix(out, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeFavourites offers saved favourite names for a first argument that
// names an existing favourite.
func (a *App) completeFavourites(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	store, err := favourites.Load(a.home)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterByPrefix(store.Names(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeHosts offers ssh host aliases for the target argument of
// "fav add <name> <host[:path]>", which is the second one.
//
// The position is checked exactly rather than as an upper bound. The first
// argument names the favourite being created, and completing it from the list
// of hosts offers names for something that does not exist yet, in the one slot
// where the user is inventing a name rather than choosing one.
func (a *App) completeHosts(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cfg, err := config.Load(a.home)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	aliases, err := sshconf.Aliases(string(a.home), cfg.SSHConfigPath())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterByPrefix(aliases, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeMounts offers the names of active mounts for a first argument.
func completeMounts(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	mounts, err := mount.Active(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(mounts))
	for _, m := range mounts {
		names = append(names, m.Name)
	}
	return filterByPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func filterByPrefix(candidates []string, prefix string) []string {
	if prefix == "" {
		return candidates
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}
