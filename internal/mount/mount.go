// Package mount runs sshfs and inspects the sshfs mounts already active.
package mount

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Errors reported when preparing or changing a mount.
var (
	ErrNoSSHFS          = errors.New("sshfs is not installed or not on PATH")
	ErrNoUnmountTool    = errors.New("no fusermount3, fusermount or umount found on PATH")
	ErrAlreadyMounted   = errors.New("already mounted")
	ErrNotMounted       = errors.New("not mounted")
	ErrTargetNotEmpty   = errors.New("mount point exists and is not empty")
	ErrEmptyHost        = errors.New("no host given")
	ErrFlagLikeArgument = errors.New("would be read by sshfs as a command line option")
)

// Spec describes one mount to create.
type Spec struct {
	Host     string
	Path     string
	Target   string
	Options  []string
	ReadOnly bool
}

// Source returns the "host:path" argument for sshfs.
//
// An empty Path leaves the source ending at the colon, which is how sshfs is
// asked for the remote home directory. Passing a literal "~" instead relies on
// the remote sftp server expanding it, which not all of them do.
func (s Spec) Source() string {
	return s.Host + ":" + s.Path
}

// Describe renders the source the way a message should read it.
//
// Source keeps the trailing colon that asks sshfs for the remote home
// directory, which is right on a command line and wrong in a sentence: it
// leaves "mounting web01:" with a colon of its own to run into.
func (s Spec) Describe() string {
	return describeSource(s.Host, s.Path)
}

// describeSource joins a host and remote path for display, dropping the
// separator when the path is the remote home directory.
func describeSource(host, path string) string {
	if path == "" {
		return host
	}
	return host + ":" + path
}

// OptionString returns the mount options as sshfs receives them.
//
// Options are comma separated, or the empty string when there are none.
func (s Spec) OptionString() string {
	opts := make([]string, 0, len(s.Options)+1)
	opts = append(opts, s.Options...)
	if s.ReadOnly {
		opts = append(opts, "ro")
	}
	return strings.Join(opts, ",")
}

// ValidateHost rejects a host that cannot be handed to sshfs as an operand.
//
// sshfs reads any argument beginning with "-" as an option, so such a host
// would silently become a flag instead of the thing being mounted.
func ValidateHost(host string) error {
	if host == "" {
		return ErrEmptyHost
	}
	if strings.HasPrefix(host, "-") {
		return fmt.Errorf("host %q: %w", host, ErrFlagLikeArgument)
	}
	return nil
}

// Validate reports whether the spec can be handed to sshfs.
//
// Run calls this, so a caller that only mounts does not have to. It is worth
// calling early where a command shows the mount before making it, so that a
// spec which will be refused is never displayed as though it were about to run.
//
// This is the check that matters, rather than the one in sshconf.Resolve: a
// host reaches sshfs from a favourites file and from a legacy import as well as
// from the command line, and only some of those pass through Resolve.
func (s Spec) Validate() error {
	if err := ValidateHost(s.Host); err != nil {
		return err
	}
	if strings.HasPrefix(s.Target, "-") {
		return fmt.Errorf("mount point %q: %w", s.Target, ErrFlagLikeArgument)
	}
	return nil
}

// Args returns the full sshfs argument list for this spec.
func (s Spec) Args() []string {
	args := []string{s.Source(), s.Target}
	if opts := s.OptionString(); opts != "" {
		args = append(args, "-o", opts)
	}
	return args
}

// CommandLine renders the sshfs invocation as a pasteable shell line.
//
// This is what smount --dry-run prints.
func (s Spec) CommandLine() string {
	parts := append([]string{"sshfs"}, s.Args()...)
	for i, p := range parts {
		if strings.ContainsAny(p, " \t\"'\\$") {
			parts[i] = "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
		}
	}
	return strings.Join(parts, " ")
}

// SSHFSPath returns the path to the sshfs binary.
func SSHFSPath() (string, error) {
	path, err := exec.LookPath("sshfs")
	if err != nil {
		return "", ErrNoSSHFS
	}
	return path, nil
}

// Run creates the mount point and mounts the spec with sshfs.
//
// sshfs inherits the standard streams because it may need to prompt for a key
// passphrase or a password, and its own diagnostics on failure are better than
// anything that could be reconstructed from an exit status.
func Run(s Spec) error {
	if err := s.Validate(); err != nil {
		return err
	}
	bin, err := SSHFSPath()
	if err != nil {
		return err
	}
	created, err := PrepareTarget(s.Target)
	if err != nil {
		return err
	}

	cmd := exec.Command(bin, s.Args()...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Only a directory this call made is safe to remove. PrepareTarget
		// accepts an empty directory that was already there, and that one
		// belongs to whoever made it.
		if created {
			cleanupTarget(s.Target)
		}
		// sshfs reports a failed ssh as a pipe error, so "connection reset by
		// peer" is what a rejected key looks like here. ssh says why, and is
		// the fastest way to find out.
		return fmt.Errorf("mounting %s: %w, try 'ssh %s' to see why", s.Describe(), err, s.Host)
	}
	return nil
}

// PrepareTarget creates the mount point and reports whether it created it.
//
// It refuses to reuse a mount point that is already in use or that holds
// files. Mounting over a non-empty directory is allowed by the kernel and
// hides whatever was there until the unmount, which is a good way to lose
// track of real data.
//
// The created flag exists so a failed mount only removes a directory this call
// made. An empty directory that was already there is somebody else's.
func PrepareTarget(target string) (created bool, err error) {
	info, err := os.Stat(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(target, 0o700); err != nil {
			return false, err
		}
		return true, nil
	case IsStale(target):
		return false, fmt.Errorf("%s: %w, from a dropped connection (run 'smount umount --force')", target, ErrAlreadyMounted)
	case err != nil:
		return false, err
	case !info.IsDir():
		return false, fmt.Errorf("%s: exists and is not a directory", target)
	}

	if _, err := Find(target); err == nil {
		return false, fmt.Errorf("%s: %w", target, ErrAlreadyMounted)
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return false, err
	}
	if len(entries) > 0 {
		return false, fmt.Errorf("%s: %w", target, ErrTargetNotEmpty)
	}
	return false, nil
}

// cleanupTarget removes an empty mount point, ignoring a directory that still
// holds anything. Only ever call it for a directory smount created.
func cleanupTarget(target string) {
	_ = os.Remove(target)
}

// ownsTarget reports whether target is a mount point smount derived, meaning it
// sits directly under the configured mount base.
//
// A mount point named with --at is the user's own directory. Removing it on
// unmount would delete something smount was only borrowing, so ownership is
// decided by where the directory sits rather than by it being empty.
func ownsTarget(target, base string) bool {
	if base == "" {
		return false
	}
	return filepath.Dir(filepath.Clean(target)) == filepath.Clean(base)
}

// UnmountTool returns the program and leading arguments for a FUSE unmount.
//
// fusermount3 comes first because fuse3 is what current distributions ship and
// a fuse2 fusermount may still be installed alongside it. umount is the last
// resort: it works on macOS and for a privileged user, but on Linux it usually
// needs root for a mount the user made themselves.
func UnmountTool(force bool) (string, []string, error) {
	for _, name := range []string{"fusermount3", "fusermount"} {
		if path, err := exec.LookPath(name); err == nil {
			if force {
				return path, []string{"-uz"}, nil
			}
			return path, []string{"-u"}, nil
		}
	}
	if path, err := exec.LookPath("umount"); err == nil {
		if force {
			return path, []string{forceUnmountFlag()}, nil
		}
		return path, nil, nil
	}
	return "", nil, ErrNoUnmountTool
}

// forceUnmountFlag returns the umount flag that detaches a mount whose
// connection has dropped.
//
// Linux spells this as the lazy unmount, -l, which detaches immediately and
// tears down once nothing is using the mount. macOS has no lazy unmount at all
// and spells force as -f; passing -l there is a usage error, which would fail
// the one case the force path exists for.
func forceUnmountFlag() string {
	if runtime.GOOS == "darwin" {
		return "-f"
	}
	return "-l"
}

// Unmount detaches a mount point and removes the directory behind it.
//
// The directory is only removed when it is one smount derived under base.
//
// force detaches a mount whose connection has dropped, which a normal unmount
// of one blocks on.
func Unmount(target, base string, force bool) error {
	bin, args, err := UnmountTool(force)
	if err != nil {
		return err
	}

	out, err := exec.Command(bin, append(args, target)...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("unmounting %s: %w", target, err)
		}
		return fmt.Errorf("unmounting %s: %s", target, msg)
	}
	if ownsTarget(target, base) {
		cleanupTarget(target)
	}
	return nil
}

// ParseTarget splits a "host[:path]" argument into its parts.
//
// A host with no colon, or with nothing after it, means the remote home
// directory.
func ParseTarget(arg string) (host, path string, err error) {
	host, path, _ = strings.Cut(arg, ":")
	if err := ValidateHost(host); err != nil {
		return "", "", err
	}
	return host, path, nil
}

// TargetFor returns the default mount point for a host and remote path.
func TargetFor(base, host, path string) string {
	return filepath.Join(base, MountName(host, path))
}

// MountName returns the mount point directory name for a host and remote path.
//
// The path is part of the name so that two directories on the same machine can
// be mounted at the same time. Naming a mount point after the host alone is
// what limited the shell function this replaces to one mount per host.
func MountName(host, path string) string {
	// A host of nothing but punctuation sanitises away entirely, and an empty
	// name would make the mount point the mount base itself, which sshfs would
	// mount over and hide every other mount point under it.
	name := sanitise(host)
	if name == "" {
		name = "host"
	}

	switch clean := cleanRemotePath(path); clean {
	case "":
		return name
	case "/":
		return name + "-root"
	default:
		// A path that sanitises away, such as "..", would otherwise leave a
		// dangling separator on the end of the name.
		if suffix := sanitise(clean); suffix != "" {
			return name + "-" + suffix
		}
		return name
	}
}

// cleanRemotePath normalises a remote path so that "/var/log", "/var/log/" and
// "var/log" all produce the same mount point name.
func cleanRemotePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "~" || path == "~/" {
		return ""
	}
	if path == "/" {
		return "/"
	}
	return strings.Trim(path, "/")
}

// sanitise reduces a string to characters that are safe and readable in a
// directory name, collapsing every run of anything else to a single dash.
func sanitise(s string) string {
	var b strings.Builder
	dashed := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
			dashed = false
		default:
			if !dashed && b.Len() > 0 {
				b.WriteByte('-')
				dashed = true
			}
		}
	}
	return strings.Trim(b.String(), "-.")
}
