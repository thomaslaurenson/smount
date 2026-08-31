package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/sshconf"
	"github.com/thomaslaurenson/smount/internal/tilde"
)

func (a *App) newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check that everything smount needs is present and working",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCheck(cmd.OutOrStdout(), a.home)
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
func runCheck(out io.Writer, home tilde.Home) error {
	var checks []result

	checks = append(checks, binaryCheck("sshfs", "required to mount anything"))
	checks = append(checks, binaryCheck("ssh", "required to resolve host settings"))
	checks = append(checks, unmountToolCheck())

	cfg, err := config.Load(home)
	if err != nil {
		checks = append(checks, result{name: "config", detail: err.Error(), failed: true})
		cfg = config.Defaults(home)
	} else {
		source := "defaults, no file written yet"
		if config.Exists(home) {
			source = home.Collapse(config.Path(home))
		}
		checks = append(checks, result{name: "config", detail: source})
	}

	checks = append(checks, sshConfigCheck(home, cfg))
	checks = append(checks, mountBaseCheck(home, cfg))
	checks = append(checks, mountsCheck()...)

	failed := 0
	for _, c := range checks {
		marker := "[ok]"
		if c.failed {
			marker = "[!!]"
			failed++
		}
		fmt.Fprintf(out, "%s %-14s %s\n", marker, c.name, c.detail)
	}
	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	return nil
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
func mountsCheck() []result {
	mounts, err := mount.Active()
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
