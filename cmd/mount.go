package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/favourites"
	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/sshconf"
	"github.com/thomaslaurenson/smount/internal/target"
	"github.com/thomaslaurenson/smount/internal/tilde"
	"github.com/thomaslaurenson/smount/internal/ui"
)

// mountOptions holds the flags shared by mounting a target and saving one.
type mountOptions struct {
	at       string
	extra    []string
	readOnly bool
	yes      bool
	dryRun   bool
	noSave   bool
}

func (o *mountOptions) register(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&o.at, "at", "", "mount point to use instead of the derived one")
	f.StringArrayVarP(&o.extra, "opt", "o", nil, "additional sshfs option, repeatable")
	f.BoolVar(&o.readOnly, "ro", false, "mount read only")
	f.BoolVarP(&o.yes, "yes", "y", false, "skip the confirmation prompt")
	f.BoolVar(&o.dryRun, "dry-run", false, "print the sshfs command instead of running it")
	f.BoolVar(&o.noSave, "no-save", false, "do not offer to save the mount as a favourite")
}

// target returns the subset of the flags that shape the mount itself, as
// opposed to the ones that shape the conversation around it.
func (o *mountOptions) target() target.Options {
	return target.Options{At: o.at, Extra: o.extra, ReadOnly: o.readOnly}
}

// runMount is the root command's action: resolve a target, confirm it, mount it.
func (a *App) runMount(cmd *cobra.Command, o *mountOptions, args []string) error {
	// Checked before anything is loaded or asked. sshfs is the whole point of
	// smount, so learning it is missing after picking a host, a path and
	// answering a confirmation wastes every one of those answers.
	//
	// --dry-run is exempt because it only prints the command. Wanting to see
	// what smount would run on a machine that cannot yet run it is a reasonable
	// thing to do, so it warns and carries on.
	if _, err := mount.SSHFSPath(); err != nil {
		if !o.dryRun {
			return err
		}
		a.ui.Warnf("%v, so this command cannot be run here", err)
	}

	cfg, err := config.Load(a.home)
	if err != nil {
		return err
	}
	store, err := favourites.Load(a.home)
	if err != nil {
		return err
	}

	var (
		spec      mount.Spec
		fromSaved bool
	)
	if len(args) == 1 {
		spec, fromSaved, err = target.Resolve(cfg, store, args[0], o.target())
	} else {
		spec, fromSaved, err = a.specInteractive(cmd.Context(), cfg, store, o)
	}
	if err != nil {
		return err
	}
	// Checked here as well as in mount.Run, so that a spec sshfs would refuse
	// is never summarised or printed by --dry-run as though it were about to
	// run. A favourite read from disk reaches this point without having passed
	// ParseTarget.
	if err := spec.Validate(); err != nil {
		return err
	}

	if !fromSaved && len(args) == 1 {
		a.warnUnknownHost(cfg, spec.Host)
	}

	// A host ssh cannot resolve is still worth trying to mount, since sshfs
	// gives a better diagnostic for it than anything reconstructed here.
	host, err := sshconf.Resolve(cmd.Context(), spec.Host)
	if err != nil {
		host = nil
	}
	// The baseline the listings compare against, so the summary only mentions
	// a resolved address where ssh really sends the connection elsewhere.
	defaults, _ := sshconf.Defaults(cmd.Context())
	summarise(cmd.ErrOrStderr(), a.home, spec, host, defaults, cfg.Options)

	if o.dryRun {
		fmt.Fprintln(cmd.OutOrStdout(), spec.CommandLine())
		return nil
	}
	if !o.yes && a.ui.Interactive() {
		proceed, err := a.ui.Confirm("Proceed with mount?", true)
		if err != nil {
			return err
		}
		if !proceed {
			return ui.ErrCancelled
		}
	}

	if err := mount.Run(cmd.Context(), spec, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
		return err
	}
	a.ui.Infof("mounted %s at %s", spec.Describe(), a.home.Collapse(spec.Target))

	if !fromSaved && !o.noSave && a.ui.Interactive() {
		return a.offerToSave(cmd, cfg, store, spec, o)
	}
	return nil
}

// warnUnknownHost notes a target that is neither a saved favourite nor an alias
// in the ssh config.
//
// It warns rather than refuses, because mounting a bare hostname or an address
// that appears in no config is legitimate. The warning earns its place because
// "ssh -G" echoes an unknown name straight back as its own hostname, so the
// summary prints a confident "Resolves to" line for a name nothing knows, and a
// typo looks exactly like a configured host.
func (a *App) warnUnknownHost(cfg *config.Config, host string) {
	known, err := sshconf.HasAlias(string(a.home), cfg.SSHConfigPath(), host)
	if err != nil || known {
		return
	}
	a.ui.Warnf("%s is not a saved favourite or a host in %s", host, cfg.SSHConfig)
}

// specInteractive walks the favourite, host and path prompts.
func (a *App) specInteractive(ctx context.Context, cfg *config.Config, store *favourites.Store, o *mountOptions) (mount.Spec, bool, error) {
	if !a.ui.Interactive() {
		return mount.Spec{}, false, errors.New("no target given and stdin is not a terminal, run 'smount <host>[:<path>]'")
	}

	if len(store.Favourites) > 0 {
		items := make([]ui.Item, 0, len(store.Favourites)+1)
		for _, fav := range store.Favourites {
			items = append(items, ui.Item{Label: fav.Name, Detail: fav.Describe()})
		}
		items = append(items, ui.Item{Label: "New mount", Detail: "choose an SSH host"})

		idx, err := a.ui.Select(ctx, "Select a favourite", items)
		if err != nil {
			return mount.Spec{}, false, err
		}
		if idx < len(store.Favourites) {
			return target.FromFavourite(cfg, &store.Favourites[idx], o.target()), true, nil
		}
	}

	host, err := a.pickHost(ctx, cfg)
	if err != nil {
		return mount.Spec{}, false, err
	}
	path, err := a.pickPath(ctx)
	if err != nil {
		return mount.Spec{}, false, err
	}
	return target.ForHost(cfg, host, path, o.target()), false, nil
}

// pickHost prompts for one of the aliases in the ssh config.
func (a *App) pickHost(ctx context.Context, cfg *config.Config) (string, error) {
	aliases, err := sshconf.Aliases(string(a.home), cfg.SSHConfigPath())
	if err != nil {
		return "", err
	}
	if len(aliases) == 0 {
		return "", fmt.Errorf("no hosts found in %s", cfg.SSHConfig)
	}

	resolved := sshconf.ResolveAll(ctx, aliases)
	// The same baseline the hosts listing uses, so that a row shows a user or
	// port only where the config sets one for that host in particular.
	defaults, _ := sshconf.Defaults(ctx)

	items := make([]ui.Item, len(aliases))
	for i, alias := range aliases {
		items[i] = ui.Item{Label: alias}
		if host := resolved[alias]; host != nil {
			items[i].Detail = host.Describe(defaults)
		}
	}

	idx, err := a.ui.Select(ctx, "Select an SSH host", items)
	if err != nil {
		return "", err
	}
	return aliases[idx], nil
}

// pickPath prompts for the remote directory to mount.
func (a *App) pickPath(ctx context.Context) (string, error) {
	items := []ui.Item{
		{Label: "Home directory", Detail: "the remote user's home"},
		{Label: "Root directory", Detail: "/"},
		{Label: "Custom path", Detail: "type a remote path"},
	}
	idx, err := a.ui.Select(ctx, "Select the remote directory", items)
	if err != nil {
		return "", err
	}

	switch idx {
	case 0:
		return "", nil
	case 1:
		return "/", nil
	}

	path, err := a.ui.Line("Remote path: ")
	if err != nil {
		return "", err
	}
	if path = strings.TrimSpace(path); path == "" {
		return "", ui.ErrCancelled
	}
	return path, nil
}

// summarise prints what is about to be mounted, immediately before the
// confirmation prompt.
//
// Only the two lines that always carry something are unconditional. The
// resolved address and the mount options are printed where they say something
// the source line and config.json do not, because this is the last thing read
// before answering a yes or no question, and a block whose lines are the same
// on every run stops being read at all.
func summarise(out io.Writer, home tilde.Home, spec mount.Spec, host, defaults *sshconf.Host, baseline []string) {
	fmt.Fprintln(out, "[*] Mount summary:")
	fmt.Fprintf(out, "      Source:      %s\n", sourceLine(spec))
	fmt.Fprintf(out, "      Mount point: %s\n", home.Collapse(spec.Target))
	if host != nil {
		if resolved := host.Describe(defaults); resolved != "" {
			fmt.Fprintf(out, "      Resolves to: %s\n", resolved)
		}
	}
	if extra := extraOptions(spec, baseline); len(extra) > 0 {
		fmt.Fprintf(out, "      Options:     %s (with the configured defaults)\n",
			strings.Join(extra, ", "))
	}
}

// sourceLine renders what is being mounted, naming the remote home directory
// rather than leaving a bare host that says nothing about which directory.
func sourceLine(spec mount.Spec) string {
	if spec.Path == "" {
		return spec.Host + " (home directory)"
	}
	return spec.Describe()
}

// extraOptions returns the options this mount adds beyond the configured
// defaults, with the read only flag among them.
//
// The baseline applies to every mount and is already written in config.json,
// so listing it here fills the widest line of the summary with something the
// user did not choose for this mount and cannot act on.
func extraOptions(spec mount.Spec, baseline []string) []string {
	seen := make(map[string]bool, len(baseline))
	for _, opt := range baseline {
		seen[opt] = true
	}

	var out []string
	for _, opt := range spec.Options {
		if seen[opt] {
			continue
		}
		seen[opt] = true
		out = append(out, opt)
	}
	if spec.ReadOnly && !seen["ro"] {
		out = append(out, "ro")
	}
	return out
}

// offerToSave asks whether a newly created ad hoc mount is worth keeping.
//
// Nothing here fails the command. The mount already succeeded, so a favourite
// that could not be written is worth a warning and nothing more.
func (a *App) offerToSave(cmd *cobra.Command, cfg *config.Config, store *favourites.Store, spec mount.Spec, o *mountOptions) error {
	save, err := a.ui.Confirm("Save this as a favourite?", false)
	if err != nil || !save {
		// A declined or cancelled prompt here leaves a working mount behind, so
		// it is not worth failing the command over.
		return nil
	}

	label, err := a.ui.Line("Favourite name: ")
	if err != nil {
		return nil
	}
	name := favourites.Slug(label)
	if name == "" {
		name = favourites.Slug(spec.Host)
	}
	if err := checkFavouriteName(cmd, name); err != nil {
		a.ui.Warnf("not saved: %v", err)
		return nil
	}
	name = store.UniqueName(name)

	if err := store.Add(target.FavouriteFor(cfg, spec, name, o.target())); err != nil {
		a.ui.Warnf("not saved: %v", err)
		return nil
	}
	if err := favourites.Save(a.home, store); err != nil {
		a.ui.Warnf("not saved: %v", err)
		return nil
	}
	a.ui.Infof("saved favourite %q", name)
	return nil
}

// checkFavouriteName rejects names that are invalid or that a subcommand
// already claims.
func checkFavouriteName(cmd *cobra.Command, name string) error {
	if err := favourites.ValidName(name); err != nil {
		return err
	}
	if reservedNames(cmd)[name] {
		return fmt.Errorf("%q is an smount subcommand, choose another name", name)
	}
	return nil
}
