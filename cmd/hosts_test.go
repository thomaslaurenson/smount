package cmd

import (
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
