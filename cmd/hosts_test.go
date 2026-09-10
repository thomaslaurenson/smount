package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHosts(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	// The header is what separates the two forms. It is checked on HOST rather
	// than on RESOLVES TO, because a config whose hosts all resolve to
	// themselves leaves that column empty and it is then dropped.
	tests := []struct {
		name       string
		args       []string
		wantHeader bool
	}{
		{name: "default lists hosts with a header", args: []string{"hosts"}, wantHeader: true},
		{name: "--short lists names alone", args: []string{"hosts", "--short"}, wantHeader: false},
		{name: "-s is the same flag", args: []string{"hosts", "-s"}, wantHeader: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, _, err := run(t, home, tc.args...)
			if err != nil {
				t.Fatalf("run(%v) error = %v", tc.args, err)
			}
			for _, host := range []string{"web01", "db-prod"} {
				if !strings.Contains(stdout, host) {
					t.Errorf("stdout = %q, want it to list %q", stdout, host)
				}
			}
			if got := strings.Contains(stdout, "HOST"); got != tc.wantHeader {
				t.Errorf("stdout has a header = %v, want %v", got, tc.wantHeader)
			}
		})
	}
}

// TestHostsDropsTheIdentityColumn guards the narrowed table. The identity was
// the same path on nearly every row, so it cost a column and said nothing.
func TestHostsDropsTheIdentityColumn(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	stdout, _, err := run(t, home, "hosts")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if strings.Contains(stdout, "IDENTITY") {
		t.Errorf("stdout = %q, want no identity column", stdout)
	}
}

func TestHostsReportsAMissingConfig(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, t.TempDir(), "hosts")
	if err == nil {
		t.Fatal("run() with no ssh config error = nil, want an error")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing written on a failure", stdout)
	}
}

// TestHostsWithNoAliases guards the stream contract for an empty listing. A
// header with nothing under it is a line a consumer has to read and discard,
// which is why the mount listing notes its own empty case on stderr.
func TestHostsWithNoAliases(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", sshDir, err)
	}
	// A wildcard block configures connections without naming one to connect
	// to, so the listing omits it and is left with nothing to print.
	ssh := "Host *\n  ServerAliveInterval 30\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(ssh), 0o600); err != nil {
		t.Fatalf("writing ssh config: %v", err)
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "the table form", args: []string{"hosts"}},
		{name: "the short form", args: []string{"hosts", "--short"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, err := run(t, home, tc.args...)
			if err != nil {
				t.Fatalf("run(%v) error = %v", tc.args, err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing written", stdout)
			}
			if !strings.Contains(stderr, "no hosts") {
				t.Errorf("stderr = %q, want the empty case noted on it", stderr)
			}
		})
	}
}
