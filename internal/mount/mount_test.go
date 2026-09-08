package mount

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestMountName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		host string
		path string
		want string
	}{
		{name: "home directory", host: "web01", path: "", want: "web01"},
		{name: "tilde is home", host: "web01", path: "~", want: "web01"},
		{name: "root", host: "web01", path: "/", want: "web01-root"},
		{name: "nested path", host: "web01", path: "/var/log", want: "web01-var-log"},
		{name: "trailing slash matches no slash", host: "web01", path: "/var/log/", want: "web01-var-log"},
		{name: "relative path", host: "web01", path: "var/log", want: "web01-var-log"},
		{name: "user at host", host: "deploy@web01", path: "", want: "deploy-web01"},
		{name: "dots kept", host: "web01.example.com", path: "", want: "web01.example.com"},
		{name: "spaces collapsed", host: "web01", path: "/my data/logs", want: "web01-my-data-logs"},
		{name: "host of only punctuation does not empty the name", host: "...", path: "", want: "host"},
		{name: "host of only dashes does not empty the name", host: "---", path: "/x", want: "host-x"},
		{name: "path of dots leaves no trailing separator", host: "web01", path: "..", want: "web01"},
		{name: "single dot path leaves no trailing separator", host: "web01", path: ".", want: "web01"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := MountName(tc.host, tc.path); got != tc.want {
				t.Errorf("MountName(%q, %q) = %q, want %q", tc.host, tc.path, got, tc.want)
			}
		})
	}
}

// TestMountNameSeparatesPaths guards the property that made a dedicated tool
// worth writing: two directories on one host have to get distinct mount points.
func TestMountNameSeparatesPaths(t *testing.T) {
	t.Parallel()
	root := MountName("web01", "/")
	logs := MountName("web01", "/var/log")
	home := MountName("web01", "")

	if root == logs || root == home || logs == home {
		t.Errorf("mount names collide: root=%q logs=%q home=%q", root, logs, home)
	}
}

func TestParseTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		arg      string
		wantHost string
		wantPath string
		wantErr  bool
	}{
		{name: "host only", arg: "web01", wantHost: "web01"},
		{name: "host and path", arg: "web01:/var/log", wantHost: "web01", wantPath: "/var/log"},
		{name: "trailing colon is home", arg: "web01:", wantHost: "web01"},
		{name: "user at host", arg: "deploy@web01:/srv", wantHost: "deploy@web01", wantPath: "/srv"},
		{name: "path containing a colon", arg: "web01:/srv/a:b", wantHost: "web01", wantPath: "/srv/a:b"},
		{name: "empty", arg: "", wantErr: true},
		{name: "colon only", arg: ":/var", wantErr: true},
		{name: "flag like host", arg: "-oProxyCommand=id", wantErr: true},
		{name: "flag like host with a path", arg: "-o:/srv", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host, path, err := ParseTarget(tc.arg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q) = nil error, want an error", tc.arg)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q) error = %v", tc.arg, err)
			}
			if host != tc.wantHost || path != tc.wantPath {
				t.Errorf("ParseTarget(%q) = (%q, %q), want (%q, %q)",
					tc.arg, host, path, tc.wantHost, tc.wantPath)
			}
		})
	}
}

func TestSpecSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec Spec
		want string
	}{
		{name: "home", spec: Spec{Host: "web01"}, want: "web01:"},
		{name: "path", spec: Spec{Host: "web01", Path: "/var/log"}, want: "web01:/var/log"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.spec.Source(); got != tc.want {
				t.Errorf("Source() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSpecArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec Spec
		want []string
	}{
		{
			name: "no options",
			spec: Spec{Host: "web01", Target: "/mnt/web01"},
			want: []string{"web01:", "/mnt/web01"},
		},
		{
			name: "options joined",
			spec: Spec{Host: "web01", Target: "/mnt/web01", Options: []string{"reconnect", "idmap=user"}},
			want: []string{"web01:", "/mnt/web01", "-o", "reconnect,idmap=user"},
		},
		{
			name: "read only appended last",
			spec: Spec{Host: "web01", Target: "/mnt/web01", Options: []string{"reconnect"}, ReadOnly: true},
			want: []string{"web01:", "/mnt/web01", "-o", "reconnect,ro"},
		},
		{
			name: "read only with no other options",
			spec: Spec{Host: "web01", Target: "/mnt/web01", ReadOnly: true},
			want: []string{"web01:", "/mnt/web01", "-o", "ro"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.spec.Args(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Args() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSpecCommandLineQuotesSpaces(t *testing.T) {
	t.Parallel()
	spec := Spec{Host: "web01", Path: "/my data", Target: "/mnt/web01"}
	got := spec.CommandLine()
	want := "sshfs 'web01:/my data' /mnt/web01"
	if got != want {
		t.Errorf("CommandLine() = %q, want %q", got, want)
	}
}

func TestTargetFor(t *testing.T) {
	t.Parallel()
	got := TargetFor("/home/me/sshfs", "web01", "/var/log")
	want := filepath.Join("/home/me/sshfs", "web01-var-log")
	if got != want {
		t.Errorf("TargetFor() = %q, want %q", got, want)
	}
}

func TestParseMountinfo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []Mount
	}{
		{
			name:  "sshfs mount",
			input: "36 25 0:35 / /home/me/sshfs/web01 rw,nosuid,nodev - fuse.sshfs web01: rw,user_id=1000\n",
			want:  []Mount{{Target: "/home/me/sshfs/web01", Source: "web01:"}},
		},
		{
			name: "other filesystems skipped",
			input: "24 30 0:22 / /proc rw - proc proc rw\n" +
				"36 25 0:35 / /mnt/a rw - fuse.sshfs web01:/srv rw\n" +
				"40 25 0:41 / /mnt/b rw - ext4 /dev/sda1 rw\n",
			want: []Mount{{Target: "/mnt/a", Source: "web01:/srv"}},
		},
		{
			name:  "optional fields before the separator",
			input: "36 25 0:35 / /mnt/a rw shared:2 master:3 - fuse.sshfs web01: rw\n",
			want:  []Mount{{Target: "/mnt/a", Source: "web01:"}},
		},
		{
			name:  "escaped space in the mount point",
			input: `36 25 0:35 / /mnt/my\040data rw - fuse.sshfs web01:/srv rw` + "\n",
			want:  []Mount{{Target: "/mnt/my data", Source: "web01:/srv"}},
		},
		{
			name:  "no separator",
			input: "garbage line without the marker\n",
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseMountinfo(strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("parseMountinfo() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseMountinfo() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseMountOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []Mount
	}{
		{
			name:  "macfuse sshfs mount",
			input: "web01:/srv on /Users/me/sshfs/web01 (macfuse, nodev, nosuid, mounted by me)\n",
			want:  []Mount{{Target: "/Users/me/sshfs/web01", Source: "web01:/srv"}},
		},
		{
			name:  "local disk skipped",
			input: "/dev/disk1s1 on / (apfs, local, journaled)\n",
			want:  nil,
		},
		{
			name:  "fuse mount without a host source skipped",
			input: "/dev/fuse on /mnt/other (fuse, rw)\n",
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseMountOutput(strings.NewReader(tc.input)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseMountOutput() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestUnescapeOctal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "no escapes", input: "/mnt/plain", want: "/mnt/plain"},
		{name: "space", input: `/mnt/my\040data`, want: "/mnt/my data"},
		{name: "tab", input: `/mnt/a\011b`, want: "/mnt/a\tb"},
		{name: "backslash", input: `/mnt/a\134b`, want: `/mnt/a\b`},
		{name: "trailing backslash left alone", input: `/mnt/a\`, want: `/mnt/a\`},
		{name: "invalid escape left alone", input: `/mnt/a\99z`, want: `/mnt/a\99z`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := unescapeOctal(tc.input); got != tc.want {
				t.Errorf("unescapeOctal(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSplitSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		source   string
		wantHost string
		wantPath string
	}{
		{name: "home", source: "web01:", wantHost: "web01"},
		{name: "path", source: "web01:/srv", wantHost: "web01", wantPath: "/srv"},
		{name: "no colon", source: "web01", wantHost: "web01"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host, path := splitSource(tc.source)
			if host != tc.wantHost || path != tc.wantPath {
				t.Errorf("splitSource(%q) = (%q, %q), want (%q, %q)",
					tc.source, host, path, tc.wantHost, tc.wantPath)
			}
		})
	}
}

func TestPrepareTarget(t *testing.T) {
	t.Parallel()

	t.Run("creates a missing directory and reports it", func(t *testing.T) {
		t.Parallel()
		target := filepath.Join(t.TempDir(), "web01")
		created, err := PrepareTarget(t.Context(), target)
		if err != nil {
			t.Fatalf("PrepareTarget() error = %v", err)
		}
		if !created {
			t.Error("PrepareTarget() created = false, want true")
		}
		if info, err := os.Stat(target); err != nil || !info.IsDir() {
			t.Fatalf("PrepareTarget() did not create a directory: %v", err)
		}
	})

	t.Run("accepts an existing empty directory without claiming it", func(t *testing.T) {
		t.Parallel()
		created, err := PrepareTarget(t.Context(), t.TempDir())
		if err != nil {
			t.Errorf("PrepareTarget() error = %v, want nil", err)
		}
		if created {
			t.Error("PrepareTarget() created = true for a directory it did not make, want false")
		}
	})

	t.Run("refuses a directory holding files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("data"), 0o600); err != nil {
			t.Fatalf("writing test file: %v", err)
		}
		if _, err := PrepareTarget(t.Context(), dir); !errors.Is(err, ErrTargetNotEmpty) {
			t.Errorf("PrepareTarget() error = %v, want %v", err, ErrTargetNotEmpty)
		}
	})

	t.Run("refuses a path that is a file", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("writing test file: %v", err)
		}
		if _, err := PrepareTarget(t.Context(), path); err == nil {
			t.Error("PrepareTarget() on a file = nil error, want an error")
		}
	})
}

func TestFindIn(t *testing.T) {
	t.Parallel()
	mounts := []Mount{
		{Name: "logs", Target: "/home/u/sshfs/logs", Source: "web01:/var/log"},
		{Name: "web01", Target: "/home/u/sshfs/web01", Source: "web01:"},
	}

	tests := []struct {
		name       string
		query      string
		wantTarget string
		wantErr    bool
	}{
		{name: "by name", query: "logs", wantTarget: "/home/u/sshfs/logs"},
		{name: "by mount point", query: "/home/u/sshfs/web01", wantTarget: "/home/u/sshfs/web01"},
		{name: "unknown name", query: "nope", wantErr: true},
		{name: "unknown mount point", query: "/elsewhere", wantErr: true},
		{name: "empty", query: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := FindIn(mounts, tc.query)
			if tc.wantErr {
				if !errors.Is(err, ErrNotMounted) {
					t.Fatalf("FindIn(%q) error = %v, want %v", tc.query, err, ErrNotMounted)
				}
				return
			}
			if err != nil {
				t.Fatalf("FindIn(%q) error = %v", tc.query, err)
			}
			if got.Target != tc.wantTarget {
				t.Errorf("FindIn(%q).Target = %q, want %q", tc.query, got.Target, tc.wantTarget)
			}
		})
	}
}

// TestFindInResolvesARelativeMountPoint covers the path form a user actually
// types: "smount umount ./sshfs/logs" from their home directory names the same
// mount as the absolute path recorded in the table.
func TestFindInResolvesARelativeMountPoint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mounts := []Mount{{Name: "logs", Target: filepath.Join(dir, "logs")}}

	got, err := FindIn(mounts, filepath.Join(dir, ".", "logs"))
	if err != nil {
		t.Fatalf("FindIn() error = %v", err)
	}
	if got.Name != "logs" {
		t.Errorf("FindIn().Name = %q, want logs", got.Name)
	}
}

// TestFindInReturnsTheStoredMount checks the result points into the slice
// passed in, so a caller can act on the state it already probed rather than a
// copy that may disagree with it.
func TestFindInReturnsTheStoredMount(t *testing.T) {
	t.Parallel()
	mounts := []Mount{{Name: "logs", Target: "/mnt/logs", State: StateStale}}

	got, err := FindIn(mounts, "logs")
	if err != nil {
		t.Fatalf("FindIn() error = %v", err)
	}
	if got != &mounts[0] {
		t.Error("FindIn() returned a copy, want a pointer into the table it was given")
	}
	if got.State != StateStale {
		t.Errorf("State = %v, want the probed state to survive the lookup", got.State)
	}
}

func TestOwnsTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target string
		base   string
		want   bool
	}{
		{name: "derived mount point", target: "/home/u/sshfs/web01", base: "/home/u/sshfs", want: true},
		{name: "trailing slash on base", target: "/home/u/sshfs/web01", base: "/home/u/sshfs/", want: true},
		{name: "named with --at elsewhere", target: "/home/u/work/logs", base: "/home/u/sshfs", want: false},
		{name: "nested below the base", target: "/home/u/sshfs/a/b", base: "/home/u/sshfs", want: false},
		{name: "base itself", target: "/home/u/sshfs", base: "/home/u/sshfs", want: false},
		{name: "sibling sharing a prefix", target: "/home/u/sshfsold/web01", base: "/home/u/sshfs", want: false},
		{name: "empty base owns nothing", target: "/home/u/sshfs/web01", base: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ownsTarget(tc.target, tc.base); got != tc.want {
				t.Errorf("ownsTarget(%q, %q) = %v, want %v", tc.target, tc.base, got, tc.want)
			}
		})
	}
}

func TestCleanupTargetLeavesNonEmptyDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("data"), 0o600); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	cleanupTarget(dir)

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("cleanupTarget() removed a non-empty directory: %v", err)
	}
}

func TestSpecValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    Spec
		wantErr error
	}{
		{name: "ordinary spec", spec: Spec{Host: "web01", Target: "/home/u/sshfs/web01"}},
		{name: "empty host", spec: Spec{Target: "/home/u/sshfs/web01"}, wantErr: ErrEmptyHost},
		{
			name:    "host sshfs would read as an option",
			spec:    Spec{Host: "-oProxyCommand=id", Target: "/home/u/sshfs/x"},
			wantErr: ErrFlagLikeArgument,
		},
		{
			name:    "mount point sshfs would read as an option",
			spec:    Spec{Host: "web01", Target: "-odebug"},
			wantErr: ErrFlagLikeArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.spec.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestRunRejectsFlagLikeHostBeforeExec guards the boundary that matters: a host
// reaching sshfs from a favourites file never passed through ParseTarget, so
// Run has to refuse it itself rather than trusting the caller.
func TestRunRejectsFlagLikeHostBeforeExec(t *testing.T) {
	t.Parallel()
	spec := Spec{Host: "-oProxyCommand=id", Target: filepath.Join(t.TempDir(), "x")}
	err := Run(t.Context(), spec, nil, io.Discard, io.Discard)
	if !errors.Is(err, ErrFlagLikeArgument) {
		t.Errorf("Run() error = %v, want %v", err, ErrFlagLikeArgument)
	}
}

func TestForceUnmountFlag(t *testing.T) {
	t.Parallel()
	// macOS has no lazy unmount; passing -l there is a usage error.
	want := "-l"
	if runtime.GOOS == "darwin" {
		want = "-f"
	}
	if got := forceUnmountFlag(); got != want {
		t.Errorf("forceUnmountFlag() on %s = %q, want %q", runtime.GOOS, got, want)
	}
}

func TestStateString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		state State
		want  string
	}{
		{name: "ok", state: StateOK, want: "ok"},
		{name: "stale", state: StateStale, want: "stale"},
		{name: "blocked", state: StateBlocked, want: "blocked"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.state.String(); got != tc.want {
				t.Errorf("State(%d).String() = %q, want %q", tc.state, got, tc.want)
			}
		})
	}
}

func TestProbeAnswersForALiveDirectory(t *testing.T) {
	t.Parallel()
	if got := probe(t.TempDir()); got != StateOK {
		t.Errorf("probe() on a plain directory = %v, want %v", got, StateOK)
	}
}

// stubUnmountTool puts a fake fusermount3 on PATH, so the unmount path can be
// exercised without a real FUSE mount to detach.
//
// The stub fails when told to, so a test can tell a refused unmount from a
// successful one.
//
// PATH is prepended rather than replaced: the stub is a shell script and still
// needs the utilities it runs.
func stubUnmountTool(t *testing.T, fail bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub relies on a shell script")
	}

	dir := t.TempDir()
	script := "#!/bin/sh\n"
	if fail {
		script += "echo 'fusermount3: entry for /x not found in /etc/mtab' >&2\nexit 1\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "fusermount3"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A mount point smount derived is its own to tidy away once it is detached.
func TestUnmountRemovesADerivedMountPoint(t *testing.T) {
	stubUnmountTool(t, false)

	base := t.TempDir()
	target := filepath.Join(base, "web01")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("creating mount point: %v", err)
	}

	if err := Unmount(t.Context(), target, base, false); err != nil {
		t.Fatalf("Unmount() error = %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Unmount() left the derived mount point behind, stat err = %v", err)
	}
}

// A mount point named with --at belongs to whoever made it, so detaching it
// must not delete it.
func TestUnmountKeepsAMountPointItDoesNotOwn(t *testing.T) {
	stubUnmountTool(t, false)

	target := t.TempDir()
	if err := Unmount(t.Context(), target, t.TempDir(), false); err != nil {
		t.Fatalf("Unmount() error = %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("Unmount() removed a mount point it does not own: %v", err)
	}
}

// Nothing is still mounted when the tool refuses, so the directory has to stay:
// removing it here would delete the mount point out from under a live mount.
func TestUnmountKeepsTheDirectoryWhenTheToolFails(t *testing.T) {
	stubUnmountTool(t, true)

	base := t.TempDir()
	target := filepath.Join(base, "web01")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("creating mount point: %v", err)
	}

	err := Unmount(t.Context(), target, base, false)
	if err == nil {
		t.Fatal("Unmount() error = nil, want the tool's refusal")
	}
	// The tool says why, and that reason is more use than the exit status.
	if !strings.Contains(err.Error(), "not found in /etc/mtab") {
		t.Errorf("Unmount() error = %v, want it to carry what the tool said", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("Unmount() removed the mount point after a failed unmount: %v", err)
	}
}
