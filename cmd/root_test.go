package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeHome builds a home directory holding an ssh config with two hosts, plus
// a favourites file when favs is not empty.
func writeHome(t *testing.T, favs string) string {
	t.Helper()
	home := t.TempDir()

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", sshDir, err)
	}
	ssh := "Host web01\nHost db-prod\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(ssh), 0o600); err != nil {
		t.Fatalf("writing ssh config: %v", err)
	}

	if favs != "" {
		smountDir := filepath.Join(home, ".smount")
		if err := os.MkdirAll(smountDir, 0o700); err != nil {
			t.Fatalf("creating %s: %v", smountDir, err)
		}
		if err := os.WriteFile(filepath.Join(smountDir, "favourites.json"), []byte(favs), 0o600); err != nil {
			t.Fatalf("writing favourites: %v", err)
		}
	}
	return home
}

// run builds the command tree over home and executes args, capturing both
// streams.
//
// A fresh tree per call rather than a shared one: flag values persist on a
// cobra command after Execute, so a reused tree leaks state between subtests.
//
// Input is /dev/null, which makes the tree non-interactive exactly as a piped
// invocation is, so a command that would otherwise prompt takes its other path.
func run(t *testing.T, home string, args ...string) (stdout, stderr string, runErr error) {
	t.Helper()

	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = in.Close() })

	var out, errOut bytes.Buffer
	root := NewRootCmd(home, in, &out, &errOut)
	root.SetArgs(args)
	runErr = root.ExecuteContext(t.Context())

	return out.String(), errOut.String(), runErr
}

// TestRootMountDryRun is the flag test for the root command. --dry-run is what
// makes it possible without sshfs installed: it prints the command that would
// run, so every other flag can be read back off that line.
func TestRootMountDryRun(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "no flags mounts the remote home under the mount base",
			args: []string{"web01", "--dry-run"},
			want: []string{"sshfs", "web01:", filepath.Join(home, "sshfs", "web01")},
		},
		{
			name: "a remote path reaches the source and the mount point",
			args: []string{"web01:/var/log", "--dry-run"},
			want: []string{"web01:/var/log", filepath.Join(home, "sshfs", "web01-var-log")},
		},
		{
			name: "--ro adds the read only option",
			args: []string{"web01", "--dry-run", "--ro"},
			want: []string{"ro"},
		},
		{
			name: "--opt adds the option given",
			args: []string{"web01", "--dry-run", "--opt", "compression=yes"},
			want: []string{"compression=yes"},
		},
		{
			name: "--at replaces the derived mount point",
			args: []string{"web01", "--dry-run", "--at", "/tmp/somewhere"},
			want: []string{"/tmp/somewhere"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, _, err := run(t, home, tc.args...)
			if err != nil {
				t.Fatalf("run(%v) error = %v", tc.args, err)
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout = %q, want it to mention %q", stdout, want)
				}
			}
		})
	}
}

// TestRootMountErrors asserts the other half of the contract: a failure is
// returned rather than printed, and nothing reaches stdout on the way out.
func TestRootMountErrors(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	tests := []struct {
		name string
		args []string
	}{
		{name: "no target with nothing to prompt", args: []string{"--dry-run"}},
		{name: "empty host", args: []string{":", "--dry-run"}},
		{name: "a host sshfs would read as a flag", args: []string{"--dry-run", "--", "-oProxyCommand=id"}},
		{name: "more than one target", args: []string{"web01", "db-prod", "--dry-run"}},
		{name: "unknown flag", args: []string{"web01", "--nope"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, _, err := run(t, home, tc.args...)
			if err == nil {
				t.Fatalf("run(%v) error = nil, want an error", tc.args)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing written on a failure", stdout)
			}
		})
	}
}

// TestRootSummaryStaysOffStdout is the guard for the stream split. The mount
// summary is conversation, so redirecting stdout has to leave the answer alone.
func TestRootSummaryStaysOffStdout(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	stdout, stderr, err := run(t, home, "web01", "--dry-run")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(stderr, "Mount summary") {
		t.Errorf("stderr = %q, want the summary on it", stderr)
	}
	if strings.Contains(stdout, "Mount summary") {
		t.Errorf("stdout = %q, want the summary kept off it", stdout)
	}
	if lines := strings.Count(strings.TrimSpace(stdout), "\n"); lines != 0 {
		t.Errorf("stdout = %q, want the sshfs command line and nothing else", stdout)
	}
}

func TestRootRejectsAnUnknownColourMode(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	_, _, err := run(t, home, "ls", "--color", "beige")

	if err == nil {
		t.Fatal("--color beige was accepted, want an error")
	}
	if !strings.Contains(err.Error(), "--color") {
		t.Errorf("error = %v, want it to name the flag", err)
	}
}
