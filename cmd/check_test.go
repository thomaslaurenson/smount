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
