package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/thomaslaurenson/smount/internal/mount"
	"github.com/thomaslaurenson/smount/internal/sshconf"
	"github.com/thomaslaurenson/smount/internal/tilde"
)

// baseline is the option set config.json applies to every mount.
var baseline = []string{"reconnect", "ServerAliveInterval=15", "follow_symlinks"}

func TestSummarise(t *testing.T) {
	t.Parallel()
	defaults := &sshconf.Host{User: "thomas", Port: "22"}

	tests := []struct {
		name     string
		spec     mount.Spec
		host     *sshconf.Host
		want     []string
		wantGone []string
	}{
		{
			name: "an ordinary mount says only what it is and where it lands",
			spec: mount.Spec{Host: "web01", Target: "/home/t/sshfs/web01", Options: baseline},
			host: &sshconf.Host{Name: "web01", HostName: "web01", User: "thomas", Port: "22"},
			want: []string{"Source:      web01 (home directory)", "Mount point: ~/sshfs/web01"},
			// The baseline is in config.json and the host resolves to itself,
			// so neither line has anything to add.
			wantGone: []string{"Resolves to", "Options"},
		},
		{
			name:     "a remote path replaces the home directory note",
			spec:     mount.Spec{Host: "web01", Path: "/var/log", Target: "/home/t/sshfs/web01-var-log", Options: baseline},
			host:     nil,
			want:     []string{"Source:      web01:/var/log"},
			wantGone: []string{"(home directory)"},
		},
		{
			name: "only the options beyond the configured ones are listed",
			spec: mount.Spec{
				Host:     "web01",
				Path:     "/srv",
				Target:   "/home/t/scratch/srv",
				Options:  append(append([]string{}, baseline...), "compression=yes"),
				ReadOnly: true,
			},
			host: nil,
			want: []string{"Options:     compression=yes, ro"},
			// The baseline must not be restated beside them.
			wantGone: []string{"reconnect", "follow_symlinks"},
		},
		{
			name: "a host ssh redirects gets a resolved line",
			spec: mount.Spec{Host: "nesi", Path: "/nesi", Target: "/home/t/sshfs/nesi", Options: baseline},
			host: &sshconf.Host{Name: "nesi", HostName: "login.mahuika.nesi.org.nz", User: "tlaurenson", Port: "22"},
			want: []string{"Resolves to: tlaurenson@login.mahuika.nesi.org.nz"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer

			summarise(&buf, tilde.Home("/home/t"), tc.spec, tc.host, defaults, baseline)

			got := buf.String()
			if !strings.HasPrefix(got, "[*] Mount summary:\n") {
				t.Errorf("summary = %q, want it to open with the heading", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("summary = %q, want %q in it", got, want)
				}
			}
			for _, gone := range tc.wantGone {
				if strings.Contains(got, gone) {
					t.Errorf("summary = %q, want %q left out", got, gone)
				}
			}
		})
	}
}

// TestSummariseStaysShort is the guard for the whole point of the change: this
// block is read immediately before a yes or no prompt.
func TestSummariseStaysShort(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	spec := mount.Spec{Host: "web01", Target: "/home/t/sshfs/web01", Options: baseline}

	summarise(&buf, tilde.Home("/home/t"), spec,
		&sshconf.Host{Name: "web01", HostName: "web01", User: "thomas", Port: "22"},
		&sshconf.Host{User: "thomas", Port: "22"}, baseline)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("summary is %d lines:\n%s\nwant 3 for an ordinary mount", len(lines), buf.String())
	}
}

func TestExtraOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec mount.Spec
		want []string
	}{
		{name: "nothing beyond the baseline", spec: mount.Spec{Options: baseline}, want: nil},
		{
			name: "one added option",
			spec: mount.Spec{Options: append(append([]string{}, baseline...), "compression=yes")},
			want: []string{"compression=yes"},
		},
		{
			name: "read only is an option like any other",
			spec: mount.Spec{Options: baseline, ReadOnly: true},
			want: []string{"ro"},
		},
		{
			// Options are layered from the config, a favourite and the command
			// line, so the same one can arrive more than once.
			name: "a repeated option is listed once",
			spec: mount.Spec{Options: []string{"compression=yes", "compression=yes"}},
			want: []string{"compression=yes"},
		},
		{
			name: "an explicit ro among the options is not repeated",
			spec: mount.Spec{Options: []string{"ro"}, ReadOnly: true},
			want: []string{"ro"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extraOptions(tc.spec, baseline)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("extraOptions() = %v, want %v", got, tc.want)
			}
		})
	}
}
