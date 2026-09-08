package cmd

import (
	"strings"
	"testing"
)

func TestHosts(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

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
			if got := strings.Contains(stdout, "RESOLVES TO"); got != tc.wantHeader {
				t.Errorf("stdout has a header = %v, want %v", got, tc.wantHeader)
			}
		})
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
