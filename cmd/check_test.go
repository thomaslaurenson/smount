package cmd

import (
	"strings"
	"testing"
)

// TestCheck asserts the report rather than the verdict. Whether the checks pass
// depends on what is installed on the machine running the test, so the contract
// under test is that every check is named and the failure count is returned as
// an error rather than printed as one.
func TestCheck(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	stdout, _, err := run(t, home, "check")
	for _, want := range []string{"sshfs", "ssh", "unmount", "config", "ssh config", "mount base", "mounts"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want a line for %q", stdout, want)
		}
	}
	if err != nil && !strings.Contains(err.Error(), "check(s) failed") {
		t.Errorf("error = %v, want it to report the failed check count", err)
	}
}

// TestCheckUsesTheMarkerVocabulary guards against the old [ok] and [!!] pair,
// which named severities no other smount output used.
func TestCheckUsesTheMarkerVocabulary(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	stdout, _, _ := run(t, home, "check")

	for _, old := range []string{"[ok]", "[!!]"} {
		if strings.Contains(stdout, old) {
			t.Errorf("stdout = %q, want %q gone", stdout, old)
		}
	}
	if !strings.Contains(stdout, "[*]") {
		t.Errorf("stdout = %q, want the info marker on a passing check", stdout)
	}
}

func TestNameColumn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		checks []result
		want   int
	}{
		{name: "no checks", checks: nil, want: 0},
		{name: "the longest name", checks: []result{{name: "ssh"}, {name: "stale mounts"}}, want: 12},
		{name: "one check", checks: []result{{name: "sshfs"}}, want: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := nameColumn(tc.checks); got != tc.want {
				t.Errorf("nameColumn() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestCheckLinesAlign is the guard for sizing the column from the data. The
// fixed width it replaced cleared the longest current name by two characters,
// so it would have misaligned the whole report on the next name added.
func TestCheckLinesAlign(t *testing.T) {
	t.Parallel()
	checks := []result{
		{name: "ssh", detail: "/usr/bin/ssh"},
		{name: "stale mounts", detail: "2 not answering", failed: true},
		{name: "a much longer check name", detail: "still aligned"},
	}
	names := nameColumn(checks)

	want := -1
	for _, c := range checks {
		line := c.line("[*]", names)
		at := strings.Index(line, c.detail)
		if at < 0 {
			t.Fatalf("line %q does not contain its detail %q", line, c.detail)
		}
		if want == -1 {
			want = at
			continue
		}
		if at != want {
			t.Errorf("detail of %q begins at column %d, want %d", c.name, at, want)
		}
	}
}
