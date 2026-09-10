package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/thomaslaurenson/smount/internal/mount"
)

// None of the tests here calls t.Parallel. stubUnmountTool sets PATH through
// t.Setenv, which mutates process-wide state and panics in a parallel test or
// under a parallel parent.

// stubUnmountTool puts a fake fusermount3 on PATH, so the unmount path can be
// exercised without a real FUSE mount to detach.
//
// It returns the file the stub appends to, one line of arguments per
// invocation, which is what lets a test see both how a mount was detached and
// how many were.
//
// PATH is prepended rather than replaced: the stub is a shell script and still
// needs the utilities it runs.
func stubUnmountTool(t *testing.T) (argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub relies on a shell script")
	}

	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + argsFile + "\n"
	if err := os.WriteFile(filepath.Join(dir, "fusermount3"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

// toolCalls returns one entry per invocation of the stubbed unmount tool, each
// the arguments it was given. A tool that was never run leaves no file at all.
func toolCalls(t *testing.T, argsFile string) []string {
	t.Helper()

	data, err := os.ReadFile(argsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("reading %s: %v", argsFile, err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// runWithMounts builds the command tree over a fixed mount table and executes
// args, capturing both streams.
//
// The table is injected because nothing else can produce one: mount.Active
// reads the kernel's mount table, and only a real sshfs mount appears there.
// Everything else is as run builds it, including a fresh tree per call.
func runWithMounts(t *testing.T, home string, mounts []mount.Mount, args ...string) (stdout, stderr string, runErr error) {
	t.Helper()

	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = in.Close() })

	var out, errOut bytes.Buffer
	a := newApp(home, in, &errOut)
	a.mounts = func(context.Context) ([]mount.Mount, error) { return mounts, nil }

	root := a.rootCmd(&out)
	root.SetArgs(args)
	runErr = root.ExecuteContext(t.Context())

	return out.String(), errOut.String(), runErr
}

// twoMounts returns a mount table under the derived mount base, with the mount
// points really present so an unmount that tidies one away has something to
// remove. One of them is not answering, which is the state that forces a lazy
// unmount of its own accord.
func twoMounts(t *testing.T, home string) []mount.Mount {
	t.Helper()

	base := filepath.Join(home, "sshfs")
	// Sorted by mount point, which is the order mount.Active returns and so the
	// order --all works through them in.
	table := []mount.Mount{
		{Name: "logs", Host: "db-prod", Path: "/var/log", Source: "db-prod:/var/log", State: mount.StateStale},
		{Name: "web01", Host: "web01", Source: "web01:", State: mount.StateOK},
	}
	for i := range table {
		table[i].Target = filepath.Join(base, table[i].Name)
		if err := os.MkdirAll(table[i].Target, 0o700); err != nil {
			t.Fatalf("creating %s: %v", table[i].Target, err)
		}
	}
	return table
}

// TestUmount is the flag test for the command. Every flag is read off what the
// unmount tool was actually invoked with, since that is the only place an
// unmount leaves a trace a test can see.
func TestUmount(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantCalls []string
	}{
		{
			name:      "a named mount is detached on its own",
			args:      []string{"umount", "web01"},
			wantCalls: []string{"-u web01"},
		},
		{
			name:      "--force detaches lazily",
			args:      []string{"umount", "web01", "--force"},
			wantCalls: []string{"-uz web01"},
		},
		{
			name:      "-f is the same flag",
			args:      []string{"umount", "web01", "-f"},
			wantCalls: []string{"-uz web01"},
		},
		{
			name:      "--all detaches every mount",
			args:      []string{"umount", "--all"},
			wantCalls: []string{"-uz logs", "-u web01"},
		},
		{
			name:      "--all and --force force every mount",
			args:      []string{"umount", "--all", "--force"},
			wantCalls: []string{"-uz logs", "-uz web01"},
		},
		{
			// A mount that has stopped answering blocks an ordinary unmount, so
			// it is forced whether or not --force was given.
			name:      "a mount that is not answering is forced anyway",
			args:      []string{"umount", "logs"},
			wantCalls: []string{"-uz logs"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			argsFile := stubUnmountTool(t)
			home := writeHome(t, "")
			mounts := twoMounts(t, home)

			stdout, _, err := runWithMounts(t, home, mounts, tc.args...)
			if err != nil {
				t.Fatalf("run(%v) error = %v", tc.args, err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want an unmount to leave it empty", stdout)
			}

			got := toolCalls(t, argsFile)
			if len(got) != len(tc.wantCalls) {
				t.Fatalf("unmount tool ran %d time(s) %v, want %d", len(got), got, len(tc.wantCalls))
			}
			// Compared on the flags and the mount name rather than the whole
			// path, which is a temporary directory that differs every run.
			for i, want := range tc.wantCalls {
				flags, name, _ := strings.Cut(want, " ")
				if !strings.HasPrefix(got[i], flags+" ") || filepath.Base(got[i]) != name {
					t.Errorf("call %d = %q, want %q against %q", i, got[i], flags, name)
				}
			}
		})
	}
}

// TestUmountErrors asserts the other half of the contract: a failure is
// returned rather than printed, and nothing reaches stdout on the way out.
func TestUmountErrors(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		mounts func(t *testing.T, home string) []mount.Mount
	}{
		{
			name:   "nothing mounted",
			args:   []string{"umount"},
			mounts: func(*testing.T, string) []mount.Mount { return nil },
		},
		{
			// Without a terminal there is no picker to choose from.
			name:   "no argument with nothing to prompt",
			args:   []string{"umount"},
			mounts: twoMounts,
		},
		{
			name:   "a name nothing is mounted under",
			args:   []string{"umount", "nosuch"},
			mounts: twoMounts,
		},
		{
			name:   "more than one name",
			args:   []string{"umount", "web01", "logs"},
			mounts: twoMounts,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubUnmountTool(t)
			home := writeHome(t, "")

			stdout, _, err := runWithMounts(t, home, tc.mounts(t, home), tc.args...)
			if err == nil {
				t.Fatalf("run(%v) error = nil, want an error", tc.args)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing written on a failure", stdout)
			}
		})
	}
}

// TestUmountReportsEachMountItDetached guards the message rather than the
// mechanism: an unmount that says nothing leaves the user unsure it happened,
// and the report belongs on stderr because it is not the command's answer.
func TestUmountReportsEachMountItDetached(t *testing.T) {
	stubUnmountTool(t)
	home := writeHome(t, "")
	mounts := twoMounts(t, home)

	_, stderr, err := runWithMounts(t, home, mounts, "umount", "--all")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	for _, want := range []string{"web01", "db-prod:/var/log"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to name %q", stderr, want)
		}
	}
}

// A derived mount point is smount's own to tidy away, and one named with --at
// is not. The command has to pass the mount base down for that to be decidable.
func TestUmountRemovesOnlyADerivedMountPoint(t *testing.T) {
	stubUnmountTool(t)
	home := writeHome(t, "")

	derived := twoMounts(t, home)[1]
	borrowed := mount.Mount{
		Name:   "elsewhere",
		Target: filepath.Join(t.TempDir(), "elsewhere"),
		Host:   "web01",
		Source: "web01:",
	}
	if err := os.MkdirAll(borrowed.Target, 0o700); err != nil {
		t.Fatalf("creating %s: %v", borrowed.Target, err)
	}

	if _, _, err := runWithMounts(t, home, []mount.Mount{derived, borrowed}, "umount", "--all"); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if _, err := os.Stat(derived.Target); !os.IsNotExist(err) {
		t.Errorf("the derived mount point survived, stat err = %v", err)
	}
	if _, err := os.Stat(borrowed.Target); err != nil {
		t.Errorf("a mount point smount does not own was removed: %v", err)
	}
}
