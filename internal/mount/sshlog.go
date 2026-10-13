package mount

import (
	"os"
	"path/filepath"
	"strings"
)

// sshLog is the file ssh writes its log to for the length of one mount call.
//
// sshfs points ssh's stderr at /dev/null whenever it runs in the background,
// which is how it always runs here, so the reason a connection failed never
// reaches the terminal and all that is left is sshfs's own "read: Connection
// reset by peer". ssh's -E writes the same log to a file instead, which is read
// back when the mount fails.
//
// A nil *sshLog is valid and captures nothing, which is what Run gets when the
// log cannot be set up, so a mount never fails for want of its diagnostics.
type sshLog struct {
	dir  string
	path string
}

// newSSHLog makes a private directory for ssh's log, or returns nil where
// capturing it would get in the way.
//
// The directory rather than a bare file in the temp directory, because ssh
// opens the log by name: in a shared /tmp, a deleted name is one another user
// can recreate before a reconnect writes to it.
func newSSHLog(opts []string) *sshLog {
	// Two ssh_command options leave sshfs using the last one, which would
	// quietly replace the user's own.
	for _, opt := range strings.Split(strings.Join(opts, ","), ",") {
		if strings.HasPrefix(opt, "ssh_command=") {
			return nil
		}
	}

	dir, err := os.MkdirTemp("", "smount-")
	if err != nil {
		return nil
	}
	path := filepath.Join(dir, "ssh.log")
	// The path is unpicked twice, by FUSE splitting -o on commas and by sshfs
	// splitting ssh_command on spaces, each with its own backslash escapes. A
	// temp directory that needs any of that, which a Windows path always does,
	// is not worth the escaping, so the mount goes ahead without the log.
	if strings.ContainsAny(path, " \t,\\") {
		_ = os.RemoveAll(dir)
		return nil
	}
	return &sshLog{dir: dir, path: path}
}

// args returns the sshfs arguments that send ssh's log to the file.
func (l *sshLog) args() []string {
	if l == nil {
		return nil
	}
	return []string{"-o", "ssh_command=ssh -E " + l.path}
}

// lines returns what ssh logged, without the carriage returns it ends each
// line with or the blank lines between them.
func (l *sshLog) lines() []string {
	if l == nil {
		return nil
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil
	}
	var out []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// remove deletes the log once the mount call is over.
//
// A mount that succeeded keeps the directory. Its ssh holds the log open for
// as long as the mount lasts, and with reconnect sshfs starts a new ssh with
// the same -E, which recreates the file but exits outright when the directory
// is missing. The empty directory is left for a reboot to clear.
func (l *sshLog) remove(mounted bool) {
	if l == nil {
		return
	}
	if mounted {
		_ = os.Remove(l.path)
		return
	}
	_ = os.RemoveAll(l.dir)
}
