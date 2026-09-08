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

// TestLsShort covers both states of the mount table for the same reason TestLs
// does. The header is what separates the two forms, so its absence is the
// assertion whether or not this machine has a mount to list.
func TestLsShort(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	stdout, stderr, err := run(t, home, "ls", "--short")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if stdout == "" {
		if !strings.Contains(stderr, "no active sshfs mounts") {
			t.Errorf("stderr = %q, want the empty case noted on it", stderr)
		}
		return
	}
	if strings.Contains(stdout, "MOUNT POINT") {
		t.Errorf("stdout = %q, want no table header", stdout)
	}
	// One bare name per line is the whole contract, so nothing on a line may
	// need splitting to be used as an argument.
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if strings.ContainsAny(line, " \t") {
			t.Errorf("line %q carries more than a name", line)
		}
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
