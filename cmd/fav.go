package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/favourites"
	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/target"
)

func (a *App) newFavCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "fav",
		Short:   "Manage saved mount targets",
		Aliases: []string{"favourite", "favourites"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.listFavourites(cmd)
		},
	}
	cmd.AddCommand(a.newFavListCmd(), a.newFavAddCmd(), a.newFavRemoveCmd(), a.newFavImportCmd())
	return cmd
}

func (a *App) newFavListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved favourites",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.listFavourites(cmd)
		},
	}
}

func (a *App) listFavourites(cmd *cobra.Command) error {
	store, err := favourites.Load(a.home)
	if err != nil {
		return err
	}
	if len(store.Favourites) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "[*] no favourites saved, run 'smount fav add' to create one")
		return nil
	}

	cfg, err := config.Load(a.home)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTARGET\tMOUNT POINT\tOPTIONS")
	for _, fav := range store.Favourites {
		spec := target.FromFavourite(cfg, &fav, target.Options{})
		options := "-"
		if len(fav.Options) > 0 || fav.ReadOnly {
			options = spec.OptionString()
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			fav.Name, fav.Describe(), a.home.Collapse(spec.Target), options)
	}
	return w.Flush()
}

func (a *App) newFavAddCmd() *cobra.Command {
	var (
		at       string
		extra    []string
		readOnly bool
	)

	cmd := &cobra.Command{
		Use:   "add <name> <host[:path]>",
		Short: "Save a favourite",
		Long: "Save a mount target under a name.\n\n" +
			"The name is how the favourite is mounted (\"smount <name>\") and, unless\n" +
			"--at says otherwise, what its mount point is called. Two favourites may\n" +
			"point at the same host, which is how one machine can have several\n" +
			"directories mounted at once.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: a.completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Not named "target": the package of that name is imported here, and
			// a local shadowing it reads as though it were in scope.
			name, dest := args[0], args[1]
			if err := checkFavouriteName(cmd, name); err != nil {
				return err
			}
			host, path, err := mount.ParseTarget(dest)
			if err != nil {
				return err
			}

			store, err := favourites.Load(a.home)
			if err != nil {
				return err
			}
			fav := favourites.Favourite{
				Name:       name,
				Host:       host,
				Path:       path,
				Mountpoint: at,
				Options:    extra,
				ReadOnly:   readOnly,
			}
			if err := store.Add(fav); err != nil {
				return err
			}
			if err := favourites.Save(a.home, store); err != nil {
				return err
			}
			a.ui.Infof("saved favourite %q", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&at, "at", "", "mount point to use instead of the derived one")
	cmd.Flags().StringArrayVarP(&extra, "opt", "o", nil, "additional sshfs option, repeatable")
	cmd.Flags().BoolVar(&readOnly, "ro", false, "always mount this favourite read only")
	return cmd
}

func (a *App) newFavRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "rm <name>",
		Short:             "Delete a favourite",
		Aliases:           []string{"remove", "delete"},
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeFavourites,
		RunE: func(_ *cobra.Command, args []string) error {
			store, err := favourites.Load(a.home)
			if err != nil {
				return err
			}
			if err := store.Remove(args[0]); err != nil {
				return err
			}
			if err := favourites.Save(a.home, store); err != nil {
				return err
			}
			a.ui.Infof("deleted favourite %q", args[0])
			return nil
		},
	}
}

func (a *App) newFavImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import",
		Short: "Import favourites from the war10ck sshfs shell function",
		Long: "Read ~/.war10ck/.sshfs_favorites and add each entry as a favourite.\n\n" +
			"The original file is left alone, so an import that picks awkward names\n" +
			"can be undone by deleting the new favourites and running it again.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			legacy := favourites.LegacyPath(a.home)
			store, err := favourites.Load(a.home)
			if err != nil {
				return err
			}

			claimed := reservedNames(cmd)
			added, err := favourites.MigrateLegacy(store, legacy, func(name string) bool {
				return claimed[name]
			})
			if err != nil {
				return err
			}
			if added == 0 {
				a.ui.Infof("nothing to import from %s", a.home.Collapse(legacy))
				return nil
			}
			if err := favourites.Save(a.home, store); err != nil {
				return err
			}
			a.ui.Infof("imported %d favourite(s) from %s", added, a.home.Collapse(legacy))
			return nil
		},
	}
}
