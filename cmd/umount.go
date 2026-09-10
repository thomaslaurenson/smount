package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/ui"
)

func (a *App) newUmountCmd() *cobra.Command {
	var all, force bool

	cmd := &cobra.Command{
		Use:               "umount [name]",
		Short:             "Unmount an sshfs mount",
		Long:              "Unmount an sshfs mount by name or mount point. With no argument, choose one interactively.",
		Args:              cobra.MaximumNArgs(1),
		Aliases:           []string{"unmount"},
		ValidArgsFunction: completeMounts,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runUmount(cmd.Context(), args, all, force)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "unmount every active sshfs mount")
	cmd.Flags().BoolVarP(&force, "force", "f", false,
		"unmount lazily, which is what clears a mount whose connection has dropped")
	return cmd
}

func (a *App) runUmount(ctx context.Context, args []string, all, force bool) error {
	cfg, err := config.Load(a.home)
	if err != nil {
		return err
	}
	mounts, err := a.mounts(ctx)
	if err != nil {
		return err
	}
	if len(mounts) == 0 {
		return errors.New("no active sshfs mounts")
	}
	// Checked before the picker for the same reason the mount path checks for
	// sshfs: choosing a mount from a list and then being told nothing can
	// unmount it wastes the choice. It comes after the empty check, since with
	// nothing mounted the missing tool is not what the user needs to hear.
	if _, _, err := mount.UnmountTool(force); err != nil {
		return err
	}

	if all {
		return a.umountAll(ctx, cfg.MountBaseDir(), mounts, force)
	}

	var target *mount.Mount
	if len(args) == 1 {
		// Searched in the table already read above rather than through
		// mount.Find, which would read and probe the whole table again.
		target, err = mount.FindIn(mounts, args[0])
		if err != nil {
			return fmt.Errorf("%s: %w", args[0], err)
		}
	} else {
		target, err = a.pickMount(ctx, mounts)
		if err != nil {
			return err
		}
	}

	return a.umountOne(ctx, cfg.MountBaseDir(), *target, force)
}

// umountAll unmounts everything, reporting failures at the end rather than
// stopping, so one wedged mount does not strand the rest.
func (a *App) umountAll(ctx context.Context, base string, mounts []mount.Mount, force bool) error {
	failed := 0
	for _, m := range mounts {
		if err := a.umountOne(ctx, base, m, force); err != nil {
			a.ui.Warnf("%v", err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d mounts could not be unmounted", failed, len(mounts))
	}
	return nil
}

func (a *App) umountOne(ctx context.Context, base string, m mount.Mount, force bool) error {
	if m.State != mount.StateOK && !force {
		a.ui.Warnf("%s is %s, unmounting lazily", m.Name, m.State)
		force = true
	}
	if err := mount.Unmount(ctx, m.Target, base, force); err != nil {
		return err
	}
	a.ui.Infof("unmounted %s from %s", m.Describe(), a.home.Collapse(m.Target))
	return nil
}

// pickMount prompts for one of the active mounts.
func (a *App) pickMount(ctx context.Context, mounts []mount.Mount) (*mount.Mount, error) {
	items := make([]ui.Item, len(mounts))
	for i, m := range mounts {
		detail := m.Source
		if m.State != mount.StateOK {
			detail += "  (" + m.State.String() + ")"
		}
		items[i] = ui.Item{Label: m.Name, Detail: detail}
	}

	idx, err := a.ui.Select(ctx, "Select a mount to unmount", items)
	if err != nil {
		return nil, err
	}
	return &mounts[idx], nil
}
