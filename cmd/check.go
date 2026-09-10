package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/sshconf"
	"github.com/thomaslaurenson/smount/internal/tilde"
	"github.com/thomaslaurenson/smount/internal/ui"
)

func (a *App) newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check that everything smount needs is present and working",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runCheck(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// result is one line of check output.
type result struct {
	name   string
	detail string
	failed bool
}

// runCheck reports on the environment and fails only when something would stop
// a mount from working. A missing favourites file or an empty mount base are
// normal on a new install, so they are reported without failing.
func (a *App) runCheck(ctx context.Context, out io.Writer) error {
	home := a.home
	var checks []result

	checks = append(checks, binaryCheck("sshfs", "required to mount anything"))
	checks = append(checks, binaryCheck("ssh", "required to resolve host settings"))
	checks = append(checks, unmountToolCheck())

	cfg, err := a.loadConfig()
	if err != nil {
		checks = append(checks, result{name: "config", detail: err.Error(), failed: true})
		cfg = config.Defaults(home)
	} else {
		// loadConfig writes the file when it is absent, so it is still missing
		// here only because that write failed, which it has already warned
		// about. The report says which settings are in force either way.
		source := home.Collapse(config.Path(home))
		if !config.Exists(home) {
			source = "defaults, " + source + " could not be written"
		}
		checks = append(checks, result{name: "config", detail: source})
	}

	checks = append(checks, sshConfigCheck(home, cfg))
	checks = append(checks, mountBaseCheck(home, cfg))
	checks = append(checks, mountsCheck(ctx)...)

	// Sized from the names actually printed. A fixed column silently misaligns
	// the whole report the first time a check with a longer name is added.
	names := nameColumn(checks)

	failed := 0
	for _, c := range checks {
		marker := ui.MarkInfo
		if c.failed {
			marker = ui.MarkWarn
			failed++
		}
		fmt.Fprintln(out, c.line(marker, names))
	}
	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	return nil
}

// line renders one check as it is printed.
//
// The detail is left to run over a narrow terminal rather than being clipped.
// It is usually a path or an error to act on, and half of one is worse than a
// wrapped line.
func (c result) line(marker string, names int) string {
	return fmt.Sprintf("%s %-*s %s", marker, names, c.name, c.detail)
}

// nameColumn returns the width the name column needs. Names are written in
// this file and are ASCII, so counting bytes is counting characters.
func nameColumn(checks []result) int {
	widest := 0
	for _, c := range checks {
		if n := len(c.name); n > widest {
			widest = n
		}
	}
	return widest
}

func binaryCheck(name, why string) result {
	path, err := exec.LookPath(name)
	if err != nil {
		return result{name: name, detail: "not on PATH, " + why, failed: true}
	}
	return result{name: name, detail: path}
}

// unmountToolCheck reports which unmount helper will be used. Only the absence
// of all of them is a failure, since any one of them is enough.
func unmountToolCheck() result {
	bin, _, err := mount.UnmountTool(false)
	if err != nil {
		return result{name: "unmount", detail: err.Error(), failed: true}
	}
	return result{name: "unmount", detail: bin}
}

func sshConfigCheck(home tilde.Home, cfg *config.Config) result {
	path := cfg.SSHConfigPath()
	aliases, err := sshconf.Aliases(string(home), path)
	if err != nil {
		return result{name: "ssh config", detail: err.Error(), failed: true}
	}
	return result{
		name:   "ssh config",
		detail: fmt.Sprintf("%s, %d host(s)", home.Collapse(path), len(aliases)),
	}
}

func mountBaseCheck(home tilde.Home, cfg *config.Config) result {
	base := cfg.MountBaseDir()
	info, err := os.Stat(base)
	switch {
	case os.IsNotExist(err):
		return result{name: "mount base", detail: home.Collapse(base) + ", created on first mount"}
	case err != nil:
		return result{name: "mount base", detail: err.Error(), failed: true}
	case !info.IsDir():
		return result{name: "mount base", detail: home.Collapse(base) + " is not a directory", failed: true}
	}
	return result{name: "mount base", detail: home.Collapse(base)}
}

// mountsCheck reports the active mounts, and flags stale ones because they need
// a forced unmount before the mount point can be reused.
func mountsCheck(ctx context.Context) []result {
	mounts, err := mount.Active(ctx)
	if err != nil {
		return []result{{name: "mounts", detail: err.Error(), failed: true}}
	}

	unhealthy := 0
	for _, m := range mounts {
		if m.State != mount.StateOK {
			unhealthy++
		}
	}
	out := []result{{name: "mounts", detail: fmt.Sprintf("%d active", len(mounts))}}
	if unhealthy > 0 {
		out = append(out, result{
			name:   "stale mounts",
			detail: fmt.Sprintf("%d not answering, clear with 'smount umount --force'", unhealthy),
			failed: true,
		})
	}
	return out
}
