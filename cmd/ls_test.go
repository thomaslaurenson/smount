package cmd

import (
	"strings"
	"testing"
)

// TestLs covers both states of the mount table, because which one a test
// machine is in is not something the test gets to decide.
func TestLs(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	stdout, stderr, err := run(t, home, "ls")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if stdout == "" {
		if !strings.Contains(stderr, "no active sshfs mounts") {
			t.Errorf("stderr = %q, want the empty case noted on it", stderr)
		}
		return
	}
	if !strings.Contains(stdout, "MOUNT POINT") {
		t.Errorf("stdout = %q, want the table header on it", stdout)
	}
}

func TestUmountWithNothingToChoose(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	// Without a terminal there is no picker, so this fails whether or not the
	// machine running it has a mount.
	stdout, _, err := run(t, home, "umount")
	if err == nil {
		t.Fatal("run() error = nil, want an error")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing written on a failure", stdout)
	}
}
