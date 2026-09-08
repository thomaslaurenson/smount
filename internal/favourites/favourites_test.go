package favourites

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/thomaslaurenson/smount/internal/tilde"
)

func TestSlug(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain word", input: "logs", want: "logs"},
		{name: "spaces become dashes", input: "work logs", want: "work-logs"},
		{name: "case folded", input: "Work Logs", want: "work-logs"},
		{name: "runs collapsed", input: "work   ---  logs", want: "work-logs"},
		{name: "edges trimmed", input: "  logs  ", want: "logs"},
		{name: "punctuation removed", input: "web01's /var/log!", want: "web01-s-var-log"},
		{name: "underscores kept", input: "work_logs", want: "work_logs"},
		{name: "digits kept", input: "web01", want: "web01"},
		{name: "empty", input: "", want: ""},
		{name: "only punctuation", input: "!!!", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Slug(tc.input); got != tc.want {
				t.Errorf("Slug(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestValidName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "plain", input: "logs"},
		{name: "with dash", input: "work-logs"},
		{name: "with underscore", input: "work_logs"},
		{name: "empty", input: "", wantErr: true},
		{name: "slash", input: "work/logs", wantErr: true},
		{name: "backslash", input: `work\logs`, wantErr: true},
		{name: "colon", input: "web01:/srv", wantErr: true},
		{name: "space", input: "work logs", wantErr: true},
		{name: "current directory", input: ".", wantErr: true},
		{name: "parent directory", input: "..", wantErr: true},
		{name: "leading dash reads as a flag", input: "-rf", wantErr: true},
		{name: "accented letter", input: "caf\u00e9", wantErr: true},
		{name: "non-latin script", input: "\u30b5\u30fc\u30d0", wantErr: true},
		{name: "emoji", input: "logs\U0001f525", wantErr: true},
		{name: "every ASCII printable that is otherwise allowed", input: "a1_-.~!@#$%^&*()+=", wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidName(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("ValidName(%q) = nil, want an error", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidName(%q) = %v, want nil", tc.input, err)
			}
		})
	}
}

func TestFavouriteDescribe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fav  Favourite
		want string
	}{
		{
			name: "host and path",
			fav:  Favourite{Host: "web01", Path: "/var/log"},
			want: "web01:/var/log",
		},
		{
			name: "remote home has no path to show",
			fav:  Favourite{Host: "web01"},
			want: "web01: (home)",
		},
		{
			name: "read only is noted",
			fav:  Favourite{Host: "web01", Path: "/var/log", ReadOnly: true},
			want: "web01:/var/log (read only)",
		},
		{
			name: "read only remote home",
			fav:  Favourite{Host: "web01", ReadOnly: true},
			want: "web01: (home) (read only)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.fav.Describe(); got != tc.want {
				t.Errorf("Describe() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStoreAddAndRemove(t *testing.T) {
	t.Parallel()
	s := &Store{Version: Version}

	if err := s.Add(Favourite{Name: "logs", Host: "web01", Path: "/var/log"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := s.Add(Favourite{Name: "logs", Host: "web02"}); !errors.Is(err, ErrExists) {
		t.Errorf("Add() duplicate error = %v, want %v", err, ErrExists)
	}
	if err := s.Add(Favourite{Name: "bad name"}); !errors.Is(err, ErrInvalidName) {
		t.Errorf("Add() invalid name error = %v, want %v", err, ErrInvalidName)
	}

	fav, err := s.Get("logs")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if fav.Host != "web01" {
		t.Errorf("Get().Host = %q, want %q", fav.Host, "web01")
	}

	if err := s.Remove("logs"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := s.Remove("logs"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove() missing error = %v, want %v", err, ErrNotFound)
	}
}

// TestStoreSeveralFavouritesPerHost covers the limitation that motivated
// keying favourites on a name: one host with several saved directories.
func TestStoreSeveralFavouritesPerHost(t *testing.T) {
	t.Parallel()
	s := &Store{Version: Version}

	if err := s.Add(Favourite{Name: "logs", Host: "web01", Path: "/var/log"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := s.Add(Favourite{Name: "srv", Host: "web01", Path: "/srv"}); err != nil {
		t.Fatalf("Add() second favourite for the same host error = %v", err)
	}
	if got := s.Names(); !reflect.DeepEqual(got, []string{"logs", "srv"}) {
		t.Errorf("Names() = %v, want [logs srv]", got)
	}
}

func TestUniqueName(t *testing.T) {
	t.Parallel()
	s := &Store{Version: Version}
	if err := s.Add(Favourite{Name: "logs", Host: "web01"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if got := s.UniqueName("other"); got != "other" {
		t.Errorf("UniqueName(other) = %q, want other", got)
	}
	if got := s.UniqueName("logs"); got != "logs-2" {
		t.Errorf("UniqueName(logs) = %q, want logs-2", got)
	}

	if err := s.Add(Favourite{Name: "logs-2", Host: "web02"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got := s.UniqueName("logs"); got != "logs-3" {
		t.Errorf("UniqueName(logs) = %q, want logs-3", got)
	}
}

// TestLoadSaveRoundTrip writes through the real file paths, so it sets HOME and
// cannot run in parallel with anything else that reads it.
func TestLoadSaveRoundTrip(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())

	empty, err := Load(home)
	if err != nil {
		t.Fatalf("Load() with no file error = %v", err)
	}
	if len(empty.Favourites) != 0 {
		t.Fatalf("Load() with no file returned %d favourites, want 0", len(empty.Favourites))
	}

	if err := empty.Add(Favourite{Name: "logs", Host: "web01", Path: "/var/log", ReadOnly: true}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := Save(home, empty); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(home)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded.Favourites, empty.Favourites) {
		t.Errorf("round trip = %+v, want %+v", loaded.Favourites, empty.Favourites)
	}
	if loaded.Version != Version {
		t.Errorf("Version = %d, want %d", loaded.Version, Version)
	}
}

func TestSaveUsesOwnerOnlyPermissions(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())

	s := &Store{Version: Version}
	if err := s.Add(Favourite{Name: "logs", Host: "web01"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := Save(home, s); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	path := Path(home)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}
