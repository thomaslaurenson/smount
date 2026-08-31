package target

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/favourites"
	"github.com/thomaslaurenson/smount/internal/tilde"
)

// testConfig returns a config with a known mount base and a short, recognisable
// option baseline, so a layering test can tell each layer apart by name.
func testConfig(home tilde.Home, base string) *config.Config {
	return &config.Config{
		MountBase: base,
		Options:   []string{"reconnect", "idmap=user"},
		SSHConfig: config.DefaultSSHConfig,
		Home:      home,
	}
}

// TestBuildLayersOptions is the test for the precedence the README documents:
// the config file sets the baseline, a favourite adds to it, and command line
// flags come last. sshfs takes the last value for a repeated option, so a later
// layer wins by appearing after an earlier one rather than by replacing it.
func TestBuildLayersOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		favOpts []string
		extra   []string
		want    []string
	}{
		{
			name: "config baseline alone",
			want: []string{"reconnect", "idmap=user"},
		},
		{
			name:    "favourite appends after the baseline",
			favOpts: []string{"compression=yes"},
			want:    []string{"reconnect", "idmap=user", "compression=yes"},
		},
		{
			name:  "command line appends after the baseline",
			extra: []string{"debug"},
			want:  []string{"reconnect", "idmap=user", "debug"},
		},
		{
			name:    "command line comes after the favourite",
			favOpts: []string{"compression=yes"},
			extra:   []string{"debug"},
			want:    []string{"reconnect", "idmap=user", "compression=yes", "debug"},
		},
		{
			name:    "a repeated option is kept twice, last one last",
			favOpts: []string{"idmap=none"},
			extra:   []string{"idmap=file"},
			want:    []string{"reconnect", "idmap=user", "idmap=none", "idmap=file"},
		},
	}

	cfg := testConfig("", "/mnt")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := build(cfg, "web01", "/var/log", "/mnt/x", tc.favOpts, Options{Extra: tc.extra})
			if !reflect.DeepEqual(got.Options, tc.want) {
				t.Errorf("Options = %v, want %v", got.Options, tc.want)
			}
		})
	}
}

// TestBuildDoesNotAliasTheConfigOptions guards the layering against a caller
// that reuses one config across several mounts. Appending onto the config's own
// slice would let the first mount's -o flags leak into the second.
func TestBuildDoesNotAliasTheConfigOptions(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "/mnt")
	baseline := append([]string(nil), cfg.Options...)

	build(cfg, "web01", "", "/mnt/a", []string{"compression=yes"}, Options{Extra: []string{"debug"}})

	if !reflect.DeepEqual(cfg.Options, baseline) {
		t.Errorf("config Options = %v after build, want %v unchanged", cfg.Options, baseline)
	}
}

func TestBuildMountPoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		host       string
		path       string
		mountpoint string
		at         string
		want       string
	}{
		{
			name: "derived from host and path",
			host: "web01", path: "/var/log",
			want: "/mnt/web01-var-log",
		},
		{
			name: "derived from host alone for the remote home",
			host: "web01",
			want: "/mnt/web01",
		},
		{
			name: "an explicit mount point is kept",
			host: "web01", path: "/var/log", mountpoint: "/mnt/logs",
			want: "/mnt/logs",
		},
		{
			name: "--at overrides a derived mount point",
			host: "web01", path: "/var/log", at: "/elsewhere",
			want: "/elsewhere",
		},
		{
			name: "--at overrides an explicit mount point",
			host: "web01", path: "/var/log", mountpoint: "/mnt/logs", at: "/elsewhere",
			want: "/elsewhere",
		},
	}

	cfg := testConfig("", "/mnt")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := build(cfg, tc.host, tc.path, tc.mountpoint, nil, Options{At: tc.at})
			if got.Target != tc.want {
				t.Errorf("Target = %q, want %q", got.Target, tc.want)
			}
		})
	}
}

func TestForHostDerivesTheMountPoint(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "/mnt")

	got := ForHost(cfg, "web01", "/var/log", Options{})

	if got.Host != "web01" || got.Path != "/var/log" {
		t.Errorf("source = %q:%q, want web01:/var/log", got.Host, got.Path)
	}
	if want := "/mnt/web01-var-log"; got.Target != want {
		t.Errorf("Target = %q, want %q", got.Target, want)
	}
}

func TestFromFavourite(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())
	cfg := testConfig(home, "~/sshfs")

	tests := []struct {
		name string
		fav  favourites.Favourite
		opts Options
		want string
	}{
		{
			name: "mount point is named after the favourite, not the host",
			fav:  favourites.Favourite{Name: "logs", Host: "web01", Path: "/var/log"},
			want: filepath.Join(string(home), "sshfs", "logs"),
		},
		{
			name: "a pinned mount point wins over the derived one",
			fav:  favourites.Favourite{Name: "logs", Host: "web01", Mountpoint: "~/elsewhere"},
			want: filepath.Join(string(home), "elsewhere"),
		},
		{
			name: "--at wins over a pinned mount point",
			fav:  favourites.Favourite{Name: "logs", Host: "web01", Mountpoint: "~/elsewhere"},
			opts: Options{At: "~/override"},
			want: filepath.Join(string(home), "override"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FromFavourite(cfg, &tc.fav, tc.opts); got.Target != tc.want {
				t.Errorf("Target = %q, want %q", got.Target, tc.want)
			}
		})
	}
}

// TestFromFavouriteReadOnlyIsAdditive covers the one option that is not simply
// appended. --ro can promote a read write favourite for a single mount, but a
// favourite saved read only stays read only whatever the command line says.
func TestFromFavouriteReadOnlyIsAdditive(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())
	cfg := testConfig(home, "~/sshfs")

	tests := []struct {
		name   string
		favRO  bool
		flagRO bool
		want   bool
	}{
		{name: "neither", favRO: false, flagRO: false, want: false},
		{name: "favourite only", favRO: true, flagRO: false, want: true},
		{name: "flag only", favRO: false, flagRO: true, want: true},
		{name: "both", favRO: true, flagRO: true, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fav := favourites.Favourite{Name: "logs", Host: "web01", ReadOnly: tc.favRO}
			got := FromFavourite(cfg, &fav, Options{ReadOnly: tc.flagRO})
			if got.ReadOnly != tc.want {
				t.Errorf("ReadOnly = %v, want %v", got.ReadOnly, tc.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "/mnt")
	store := &favourites.Store{Favourites: []favourites.Favourite{
		{Name: "logs", Host: "web01", Path: "/var/log"},
		// Named after a host so that the lookup order is observable.
		{Name: "db-prod", Host: "elsewhere", Path: "/data"},
	}}

	tests := []struct {
		name      string
		arg       string
		wantHost  string
		wantPath  string
		wantSaved bool
		wantErr   bool
	}{
		{name: "favourite by name", arg: "logs", wantHost: "web01", wantPath: "/var/log", wantSaved: true},
		{
			name: "a favourite is preferred to a host of the same name",
			arg:  "db-prod", wantHost: "elsewhere", wantPath: "/data", wantSaved: true,
		},
		{name: "host and path", arg: "web01:/var/log", wantHost: "web01", wantPath: "/var/log"},
		{name: "host alone", arg: "web01", wantHost: "web01"},
		{name: "host with a trailing colon", arg: "web01:", wantHost: "web01"},
		{name: "user at host", arg: "root@web01:/etc", wantHost: "root@web01", wantPath: "/etc"},
		{name: "flag like host is refused", arg: "-o:/etc", wantErr: true},
		{name: "empty host is refused", arg: ":/etc", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec, fromSaved, err := Resolve(cfg, store, tc.arg, Options{})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Resolve(%q) = nil error, want an error", tc.arg)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", tc.arg, err)
			}
			if spec.Host != tc.wantHost || spec.Path != tc.wantPath {
				t.Errorf("source = %q:%q, want %q:%q", spec.Host, spec.Path, tc.wantHost, tc.wantPath)
			}
			if fromSaved != tc.wantSaved {
				t.Errorf("fromSaved = %v, want %v", fromSaved, tc.wantSaved)
			}
		})
	}
}

func TestFavouriteFor(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())
	cfg := testConfig(home, "~/sshfs")

	t.Run("keeps only the command line option layer", func(t *testing.T) {
		spec := ForHost(cfg, "web01", "/var/log", Options{Extra: []string{"debug"}})

		got := FavouriteFor(cfg, spec, "logs", Options{Extra: []string{"debug"}})

		if !reflect.DeepEqual(got.Options, []string{"debug"}) {
			t.Errorf("Options = %v, want [debug]; the config baseline must not be frozen in", got.Options)
		}
		if got.Host != "web01" || got.Path != "/var/log" {
			t.Errorf("source = %q:%q, want web01:/var/log", got.Host, got.Path)
		}
	})

	t.Run("does not pin a mount point it would derive anyway", func(t *testing.T) {
		spec := ForHost(cfg, "web01", "", Options{At: filepath.Join(string(home), "sshfs", "logs")})

		got := FavouriteFor(cfg, spec, "logs", Options{})

		if got.Mountpoint != "" {
			t.Errorf("Mountpoint = %q, want it left empty so mount_base still moves it", got.Mountpoint)
		}
	})

	t.Run("pins a mount point that differs from the derived one", func(t *testing.T) {
		spec := ForHost(cfg, "web01", "", Options{At: filepath.Join(string(home), "elsewhere")})

		got := FavouriteFor(cfg, spec, "logs", Options{})

		if want := "~/elsewhere"; got.Mountpoint != want {
			t.Errorf("Mountpoint = %q, want %q", got.Mountpoint, want)
		}
	})

	// A mount point outside the home directory has no ~ form, so it is pinned
	// as the absolute path it already is.
	t.Run("pins an absolute mount point outside home unchanged", func(t *testing.T) {
		spec := ForHost(cfg, "web01", "", Options{At: "/srv/logs"})

		got := FavouriteFor(cfg, spec, "logs", Options{})

		if got.Mountpoint != "/srv/logs" {
			t.Errorf("Mountpoint = %q, want /srv/logs", got.Mountpoint)
		}
	})
}

// TestFavouriteForRoundTrip checks the two halves agree: a favourite saved from
// a spec has to mount back to the same place, or "save this as a favourite"
// quietly changes where the mount lands next time.
func TestFavouriteForRoundTrip(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())
	cfg := testConfig(home, "~/sshfs")

	tests := []struct {
		name string
		opts Options
	}{
		{name: "derived mount point", opts: Options{}},
		{name: "pinned mount point", opts: Options{At: filepath.Join(string(home), "elsewhere")}},
		{name: "with command line options", opts: Options{Extra: []string{"debug"}}},
		{name: "read only", opts: Options{ReadOnly: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := ForHost(cfg, "web01", "/var/log", tc.opts)
			fav := FavouriteFor(cfg, spec, "logs", tc.opts)

			got := FromFavourite(cfg, &fav, Options{})

			if got.Target != spec.Target {
				t.Errorf("Target = %q after a round trip, want %q", got.Target, spec.Target)
			}
			if got.ReadOnly != spec.ReadOnly {
				t.Errorf("ReadOnly = %v after a round trip, want %v", got.ReadOnly, spec.ReadOnly)
			}
			if got.OptionString() != spec.OptionString() {
				t.Errorf("options = %q after a round trip, want %q", got.OptionString(), spec.OptionString())
			}
		})
	}
}
