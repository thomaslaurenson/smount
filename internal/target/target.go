// Package target turns what the user asked to mount into a mount.Spec.
//
// It is the layer between the command line and sshfs: it decides which of a
// favourite, the ssh config and the command line supplies each part of a
// mount, and in what order their options are applied.
package target

import (
	"path/filepath"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/favourites"
	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/tilde"
)

// Options are the command line choices that add to, or override, what the
// config file and a favourite supply.
type Options struct {
	At       string
	Extra    []string
	ReadOnly bool
}

// Resolve turns a single command line argument into a spec, reporting whether
// it came from a saved favourite.
//
// A favourite name is looked up first, so "smount logs" mounts the favourite
// rather than a host that happens to share its name. Anything that is not a
// favourite is read as a "host[:path]" pair.
func Resolve(cfg *config.Config, store *favourites.Store, arg string, o Options) (mount.Spec, bool, error) {
	if fav, err := store.Get(arg); err == nil {
		return FromFavourite(cfg, fav, o), true, nil
	}
	host, path, err := mount.ParseTarget(arg)
	if err != nil {
		return mount.Spec{}, false, err
	}
	return ForHost(cfg, host, path, o), false, nil
}

// FromFavourite builds a spec from a saved favourite.
//
// The mount point is named after the favourite rather than derived from the
// host, which is what lets two favourites point at the same machine.
//
// A favourite's own read only setting is added to the command line one rather
// than replacing it, so --ro can promote a read write favourite for one mount
// but nothing can quietly demote a favourite saved as read only.
func FromFavourite(cfg *config.Config, fav *favourites.Favourite, o Options) mount.Spec {
	mountpoint := tilde.Expand(fav.Mountpoint)
	if mountpoint == "" {
		mountpoint = filepath.Join(cfg.MountBaseDir(), fav.Name)
	}
	spec := build(cfg, fav.Host, fav.Path, mountpoint, fav.Options, o)
	spec.ReadOnly = spec.ReadOnly || fav.ReadOnly
	return spec
}

// ForHost builds a spec for an ad hoc host and remote path, with the mount
// point derived from both.
func ForHost(cfg *config.Config, host, path string, o Options) mount.Spec {
	return build(cfg, host, path, "", nil, o)
}

// build assembles a spec, layering mount options so that the config file sets
// the baseline, a favourite adds to it, and command line flags come last. sshfs
// takes the last value for a repeated option, so later layers win.
//
// An empty mountpoint is derived from the host and remote path. --at replaces
// whatever was going to be used, derived or not.
func build(cfg *config.Config, host, path, mountpoint string, favOpts []string, o Options) mount.Spec {
	switch {
	case o.At != "":
		mountpoint = tilde.Expand(o.At)
	case mountpoint == "":
		mountpoint = mount.TargetFor(cfg.MountBaseDir(), host, path)
	}

	opts := make([]string, 0, len(cfg.Options)+len(favOpts)+len(o.Extra))
	opts = append(opts, cfg.Options...)
	opts = append(opts, favOpts...)
	opts = append(opts, o.Extra...)

	return mount.Spec{
		Host:     host,
		Path:     path,
		Target:   mountpoint,
		Options:  opts,
		ReadOnly: o.ReadOnly,
	}
}

// FavouriteFor builds the favourite that saves an ad hoc mount under name.
//
// Only the command line option layer is kept. The config baseline is applied
// again on every mount, so storing it here would freeze today's defaults into
// the favourite.
//
// The mount point is pinned only when it is not the one this favourite would
// derive anyway, so that changing mount_base still moves the default ones.
func FavouriteFor(cfg *config.Config, spec mount.Spec, name string, o Options) favourites.Favourite {
	fav := favourites.Favourite{
		Name:     name,
		Host:     spec.Host,
		Path:     spec.Path,
		Options:  o.Extra,
		ReadOnly: spec.ReadOnly,
	}
	if derived := filepath.Join(cfg.MountBaseDir(), name); derived != spec.Target {
		fav.Mountpoint = tilde.Collapse(spec.Target)
	}
	return fav
}
