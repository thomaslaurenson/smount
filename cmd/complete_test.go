package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

// These tests set HOME, which is process wide state, so they do not call
// t.Parallel().

// fakeHome points HOME at a temporary directory holding an ssh config with two
// hosts and a favourites file with two entries.
func fakeHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", sshDir, err)
	}
	ssh := "Host web01\nHost db-prod\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(ssh), 0o600); err != nil {
		t.Fatalf("writing ssh config: %v", err)
	}

	smountDir := filepath.Join(home, ".smount")
	if err := os.MkdirAll(smountDir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", smountDir, err)
	}
	favs := `{"version":1,"favourites":[{"name":"logs","host":"web01"},{"name":"backup","host":"db-prod"}]}`
	if err := os.WriteFile(filepath.Join(smountDir, "favourites.json"), []byte(favs), 0o600); err != nil {
		t.Fatalf("writing favourites: %v", err)
	}
}

// TestCompleteHostsOnlyCompletesTheTargetArgument is the guard for the argument
// position. "fav add" takes a new name then a host, and offering host aliases
// for the name slot proposes names for something that does not exist yet.
func TestCompleteHostsOnlyCompletesTheTargetArgument(t *testing.T) {
	fakeHome(t)

	tests := []struct {
		name       string
		args       []string
		toComplete string
		want       []string
	}{
		{name: "name argument gets nothing", args: nil, want: nil},
		{
			name: "target argument gets the aliases",
			args: []string{"myfav"},
			want: []string{"db-prod", "web01"},
		},
		{
			name: "target argument is filtered by prefix",
			args: []string{"myfav"}, toComplete: "web",
			want: []string{"web01"},
		},
		{name: "past the last argument gets nothing", args: []string{"myfav", "web01"}, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, directive := completeHosts(nil, tc.args, tc.toComplete)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("completeHosts(%v, %q) = %v, want %v", tc.args, tc.toComplete, got, tc.want)
			}
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Errorf("directive = %v, want NoFileComp", directive)
			}
		})
	}
}

func TestCompleteTargets(t *testing.T) {
	fakeHome(t)

	tests := []struct {
		name       string
		args       []string
		toComplete string
		want       []string
	}{
		{
			name: "favourites first, then hosts",
			want: []string{"backup", "logs", "db-prod", "web01"},
		},
		{
			name:       "filtered by prefix across both sources",
			toComplete: "b",
			want:       []string{"backup"},
		},
		{name: "only one target is taken", args: []string{"web01"}, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, directive := completeTargets(nil, tc.args, tc.toComplete)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("completeTargets(%v, %q) = %v, want %v", tc.args, tc.toComplete, got, tc.want)
			}
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Errorf("directive = %v, want NoFileComp", directive)
			}
		})
	}
}

func TestCompleteFavourites(t *testing.T) {
	fakeHome(t)

	tests := []struct {
		name       string
		args       []string
		toComplete string
		want       []string
	}{
		{name: "every favourite", want: []string{"backup", "logs"}},
		{name: "filtered by prefix", toComplete: "l", want: []string{"logs"}},
		{name: "only one name is taken", args: []string{"logs"}, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, directive := completeFavourites(nil, tc.args, tc.toComplete)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("completeFavourites(%v, %q) = %v, want %v", tc.args, tc.toComplete, got, tc.want)
			}
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Errorf("directive = %v, want NoFileComp", directive)
			}
		})
	}
}

func TestCompleteMountsTakesOneArgument(t *testing.T) {
	fakeHome(t)

	got, directive := completeMounts(nil, []string{"already"}, "")
	if got != nil {
		t.Errorf("completeMounts() past the last argument = %v, want nothing", got)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want NoFileComp", directive)
	}
}

// TestCompletersNeverFallBackToFilenames covers the directive rather than the
// suggestions. ShellCompDirectiveError makes the shell complete filenames
// instead, and none of these arguments is ever a local path, so an unreadable
// config must answer with nothing rather than with the working directory.
func TestCompletersNeverFallBackToFilenames(t *testing.T) {
	// A home with no ssh config, no favourites file and no config file, so
	// every underlying load either fails or comes back empty.
	t.Setenv("HOME", t.TempDir())

	completers := map[string]func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective){
		"completeTargets":    completeTargets,
		"completeFavourites": completeFavourites,
		"completeHosts":      completeHosts,
		"completeMounts":     completeMounts,
	}

	for name, complete := range completers {
		t.Run(name, func(t *testing.T) {
			for _, args := range [][]string{nil, {"one"}, {"one", "two"}} {
				_, directive := complete(nil, args, "")
				if directive&cobra.ShellCompDirectiveError != 0 {
					t.Errorf("%s(%v) returned ShellCompDirectiveError, want no filename fallback", name, args)
				}
			}
		})
	}
}
