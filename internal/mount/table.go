package mount

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// mountinfoPath is the kernel's view of this process's mount table. It is
// preferred over /etc/mtab and /proc/mounts because its fields are unambiguous
// and it cannot go stale.
const mountinfoPath = "/proc/self/mountinfo"

// probeTimeout bounds how long one staleness check may block.
//
// A dropped connection answers straight away with ENOTCONN, so this only ever
// expires for a mount that is not answering at all.
const probeTimeout = 250 * time.Millisecond

// State describes whether a mount point is answering.
type State int

const (
	// StateOK is a mount that answered.
	StateOK State = iota
	// StateStale is a mount whose connection dropped, which answers ENOTCONN.
	StateStale
	// StateBlocked is a mount that did not answer within probeTimeout, which is
	// what a mount does when its peer stops responding rather than closing.
	StateBlocked
)

// String returns the word smount prints for a state.
func (s State) String() string {
	switch s {
	case StateStale:
		return "stale"
	case StateBlocked:
		return "blocked"
	default:
		return "ok"
	}
}

// Mount describes one active sshfs mount.
type Mount struct {
	// Name is the last element of Target, which is how smount refers to a
	// mount on the command line.
	Name   string
	Target string
	Source string
	Host   string
	Path   string
	State  State
}

// Describe renders the mount for a message.
//
// It drops the trailing colon that a remote home directory carries in the
// recorded source.
func (m Mount) Describe() string {
	return describeSource(m.Host, m.Path)
}

// UnderBase reports whether smount derived this mount point, meaning it sits
// directly under base rather than having been named with --at.
func (m Mount) UnderBase(base string) bool {
	return ownsTarget(m.Target, base)
}

// Active returns every sshfs mount visible to this process.
//
// Mounts are sorted by mount point.
func Active(ctx context.Context) ([]Mount, error) {
	var (
		mounts []Mount
		err    error
	)
	if file, openErr := os.Open(mountinfoPath); openErr == nil {
		defer file.Close()
		mounts, err = parseMountinfo(file)
	} else {
		mounts, err = activeFromCommand(ctx)
	}
	if err != nil {
		return nil, err
	}

	for i := range mounts {
		mounts[i].Name = filepath.Base(mounts[i].Target)
		mounts[i].Host, mounts[i].Path = splitSource(mounts[i].Source)
		mounts[i].State = probe(mounts[i].Target)
	}
	slices.SortFunc(mounts, func(a, b Mount) int { return cmp.Compare(a.Target, b.Target) })
	return mounts, nil
}

// Find returns the active mount matching name, reading the mount table itself.
//
// name may be a mount point path or the final element of one.
func Find(ctx context.Context, name string) (*Mount, error) {
	mounts, err := Active(ctx)
	if err != nil {
		return nil, err
	}
	return FindIn(mounts, name)
}

// FindIn returns the mount in mounts matching name.
//
// It takes the table rather than reading it, so a caller that has already
// listed the mounts does not pay for a second scan. Active probes every mount
// point, and a probe of one that has stopped answering costs the full timeout,
// so the second read is the expensive one precisely when something is wrong.
func FindIn(mounts []Mount, name string) (*Mount, error) {
	target, err := filepath.Abs(name)
	if err != nil {
		target = name
	}
	for i := range mounts {
		if mounts[i].Name == name || mounts[i].Target == name || mounts[i].Target == target {
			return &mounts[i], nil
		}
	}
	return nil, ErrNotMounted
}

// IsStale reports whether a mount point's connection has dropped.
//
// A dead sshfs mount stays in the mount table, so it still looks mounted. Only
// touching it reveals the state, as ENOTCONN.
//
// This waits as long as it takes, which is right when smount is about to act on
// one mount point the user named. Listing every mount uses probe instead.
func IsStale(target string) bool {
	_, err := os.Stat(target)
	return errors.Is(err, syscall.ENOTCONN)
}

// probe reports the state of one mount point without blocking indefinitely.
//
// Statting a mount whose peer has stopped responding blocks in the kernel and
// cannot be interrupted, so the stat runs in its own goroutine and is abandoned
// when it takes too long. Waiting instead would hang every command that lists
// mounts, including the umount that would clear the offending one.
//
// Abandoning it leaks a goroutine per unresponsive mount. That is the price of
// a command that returns, and smount is short lived enough never to accumulate
// them.
func probe(target string) State {
	done := make(chan State, 1)
	go func() {
		if _, err := os.Stat(target); errors.Is(err, syscall.ENOTCONN) {
			done <- StateStale
			return
		}
		done <- StateOK
	}()

	select {
	case state := <-done:
		return state
	case <-time.After(probeTimeout):
		return StateBlocked
	}
}

// parseMountinfo reads sshfs entries out of the /proc/self/mountinfo format.
//
// Optional fields sit between the mount options and the separator, and there
// can be any number of them, so the fields after the separator have to be
// located by finding " - " rather than by counting from the start of the line.
func parseMountinfo(r io.Reader) ([]Mount, error) {
	var mounts []Mount
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		sep := strings.Index(line, " - ")
		if sep < 0 {
			continue
		}
		before := strings.Fields(line[:sep])
		after := strings.Fields(line[sep+len(" - "):])
		if len(before) < 5 || len(after) < 2 {
			continue
		}
		if after[0] != "fuse.sshfs" {
			continue
		}
		mounts = append(mounts, Mount{
			Target: unescapeOctal(before[4]),
			Source: unescapeOctal(after[1]),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return mounts, nil
}

// activeFromCommand reads the mount table on systems without mountinfo, which
// in practice means macOS with macFUSE.
func activeFromCommand(ctx context.Context) ([]Mount, error) {
	out, err := exec.CommandContext(ctx, "mount").Output()
	if err != nil {
		return nil, err
	}
	return parseMountOutput(strings.NewReader(string(out))), nil
}

// parseMountOutput reads the "source on target (type, ...)" format that the
// mount command prints on BSD derived systems.
//
// macFUSE reports its type as macfuse or osxfuse rather than fuse.sshfs, and
// neither name says which FUSE filesystem is behind it. The source having a
// host:path shape is what identifies an sshfs mount here.
func parseMountOutput(r io.Reader) []Mount {
	var mounts []Mount
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		open := strings.LastIndex(line, " (")
		if open < 0 || !strings.HasSuffix(line, ")") {
			continue
		}
		fstype, _, _ := strings.Cut(line[open+2:len(line)-1], ",")
		if !strings.Contains(strings.ToLower(fstype), "fuse") {
			continue
		}
		source, target, found := strings.Cut(line[:open], " on ")
		if !found || !strings.Contains(source, ":") {
			continue
		}
		mounts = append(mounts, Mount{Target: target, Source: source})
	}
	return mounts
}

// splitSource breaks an sshfs source into its host and remote path. An empty
// path means the remote home directory, which is what sshfs mounts when the
// source ends at the colon.
func splitSource(source string) (host, path string) {
	host, path, found := strings.Cut(source, ":")
	if !found {
		return source, ""
	}
	return host, path
}

// unescapeOctal decodes the \NNN escapes the kernel writes for characters that
// would otherwise break the whitespace separated mountinfo format.
func unescapeOctal(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
