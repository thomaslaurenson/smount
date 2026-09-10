package cmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/tilde"
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
	// SOURCE is the one column that always carries something, so it is the
	// header that survives however unremarkable the mounts are.
	if !strings.Contains(stdout, "SOURCE") {
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

// TestLsRows is the guard for the cells the listing leaves empty, and for the
// one it must not: a mount of a remote home directory has to name that
// directory rather than repeating the name column.
func TestLsRows(t *testing.T) {
	t.Parallel()
	const base = "/home/t/sshfs"
	home := tilde.Home("/home/t")

	tests := []struct {
		name  string
		mount mount.Mount
		want  []string
	}{
		{
			name:  "a home directory mount names the directory",
			mount: mount.Mount{Name: "web01", Target: "/home/t/sshfs/web01", Host: "web01"},
			want:  []string{"web01", "", "web01:~", ""},
		},
		{
			name: "a remote path is shown as it is",
			mount: mount.Mount{
				Name: "web01-var-log", Target: "/home/t/sshfs/web01-var-log",
				Host: "web01", Path: "/var/log",
			},
			want: []string{"web01-var-log", "", "web01:/var/log", ""},
		},
		{
			name: "a mount point smount did not derive is shown",
			mount: mount.Mount{
				Name: "logs", Target: "/home/t/scratch/logs",
				Host: "db-prod", Path: "/var/lib",
			},
			want: []string{"logs", "~/scratch/logs", "db-prod:/var/lib", ""},
		},
		{
			name: "a mount that is not answering carries a status",
			mount: mount.Mount{
				Name: "web01", Target: "/home/t/sshfs/web01",
				Host: "web01", State: mount.StateStale,
			},
			want: []string{"web01", "", "web01:~", "stale"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := lsRows([]mount.Mount{tc.mount}, base, home)

			if len(got) != 1 {
				t.Fatalf("lsRows() returned %d rows, want 1", len(got))
			}
			if !slices.Equal(got[0], tc.want) {
				t.Errorf("lsRows() = %q, want %q", got[0], tc.want)
			}
		})
	}
}

// TestLsRowsDoNotRepeatTheName is the guard for the whole point of the source
// column: a cell that is the name over again costs a column and says nothing.
func TestLsRowsDoNotRepeatTheName(t *testing.T) {
	t.Parallel()
	mounts := []mount.Mount{
		{Name: "web01", Target: "/home/t/sshfs/web01", Host: "web01"},
		{Name: "db-prod", Target: "/home/t/sshfs/db-prod", Host: "db-prod"},
	}

	for _, row := range lsRows(mounts, "/home/t/sshfs", tilde.Home("/home/t")) {
		if row[2] == row[0] {
			t.Errorf("row = %q, want the source to say more than the name", row)
		}
	}
}
