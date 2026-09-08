package cmd

import (
	"strings"
	"testing"
)

const twoFavourites = `{"version":1,"favourites":[` +
	`{"name":"logs","host":"web01","path":"/var/log"},` +
	`{"name":"backup","host":"db-prod"}]}`

// TestFavIsMountedByName is the reason favourites exist: "smount <name>" has to
// resolve the favourite rather than a host of the same name.
func TestFavIsMountedByName(t *testing.T) {
	t.Parallel()
	home := writeHome(t, twoFavourites)

	stdout, _, err := run(t, home, "logs", "--dry-run")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(stdout, "web01:/var/log") {
		t.Errorf("stdout = %q, want the favourite's target", stdout)
	}
	// A favourite mounts under its own name, which is what lets two of them
	// point at one machine.
	if !strings.Contains(stdout, "sshfs/logs") {
		t.Errorf("stdout = %q, want the mount point named after the favourite", stdout)
	}
}
