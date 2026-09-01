package cmd

import (
	"strings"
	"testing"
)

const twoFavourites = `{"version":1,"favourites":[` +
	`{"name":"logs","host":"web01","path":"/var/log"},` +
	`{"name":"backup","host":"db-prod"}]}`

func TestFavList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		favs       string
		args       []string
		wantStdout []string
		wantStderr string
	}{
		{
			name:       "with favourites saved",
			favs:       twoFavourites,
			args:       []string{"fav", "list"},
			wantStdout: []string{"NAME", "logs", "web01:/var/log", "backup"},
		},
		{
			name:       "the bare fav command lists them too",
			favs:       twoFavourites,
			args:       []string{"fav"},
			wantStdout: []string{"logs", "backup"},
		},
		{
			name:       "with none saved",
			args:       []string{"fav", "list"},
			wantStderr: "no favourites saved",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, err := run(t, writeHome(t, tc.favs), tc.args...)
			if err != nil {
				t.Fatalf("run(%v) error = %v", tc.args, err)
			}
			for _, want := range tc.wantStdout {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout = %q, want it to mention %q", stdout, want)
				}
			}
			if tc.wantStderr != "" {
				if stdout != "" {
					t.Errorf("stdout = %q, want the empty case kept off it", stdout)
				}
				if !strings.Contains(stderr, tc.wantStderr) {
					t.Errorf("stderr = %q, want it to mention %q", stderr, tc.wantStderr)
				}
			}
		})
	}
}

// TestFavAddThenList walks the round trip, because a favourite that saves and
// cannot be listed back is the failure this pair exists to catch.
func TestFavAddThenList(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	if _, _, err := run(t, home, "fav", "add", "logs", "web01:/var/log", "--ro"); err != nil {
		t.Fatalf("fav add error = %v", err)
	}

	stdout, _, err := run(t, home, "fav", "list")
	if err != nil {
		t.Fatalf("fav list error = %v", err)
	}
	if !strings.Contains(stdout, "logs") || !strings.Contains(stdout, "web01:/var/log") {
		t.Errorf("stdout = %q, want the saved favourite listed", stdout)
	}
	if !strings.Contains(stdout, "ro") {
		t.Errorf("stdout = %q, want the read only flag kept with the favourite", stdout)
	}

	if _, _, err := run(t, home, "fav", "rm", "logs"); err != nil {
		t.Fatalf("fav rm error = %v", err)
	}
	stdout, stderr, err := run(t, home, "fav", "list")
	if err != nil {
		t.Fatalf("fav list after rm error = %v", err)
	}
	if stdout != "" || !strings.Contains(stderr, "no favourites saved") {
		t.Errorf("stdout, stderr = %q, %q after rm, want the empty case", stdout, stderr)
	}
}

func TestFavErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		favs string
		args []string
	}{
		{name: "a name a subcommand already claims", args: []string{"fav", "add", "ls", "web01"}},
		{name: "a name that is not usable", args: []string{"fav", "add", "a/b", "web01"}},
		{name: "a host sshfs would read as a flag", args: []string{"fav", "add", "logs", "-oProxyCommand=id"}},
		{name: "a duplicate name", favs: twoFavourites, args: []string{"fav", "add", "logs", "web01"}},
		{name: "removing one that is not there", args: []string{"fav", "rm", "nope"}},
		{name: "too few arguments", args: []string{"fav", "add", "logs"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, _, err := run(t, writeHome(t, tc.favs), tc.args...)
			if err == nil {
				t.Fatalf("run(%v) error = nil, want an error", tc.args)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing written on a failure", stdout)
			}
		})
	}
}

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

func TestFavImportWithNothingToImport(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	stdout, stderr, err := run(t, home, "fav", "import")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want the note kept off it", stdout)
	}
	if !strings.Contains(stderr, "nothing to import") {
		t.Errorf("stderr = %q, want it to say there was nothing to import", stderr)
	}
}
