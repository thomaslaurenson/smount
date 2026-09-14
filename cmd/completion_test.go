package cmd

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/tilde"
)

// fakeHome returns an App reading a home directory with two ssh hosts and two
// saved favourites, which is what every completer here has to offer.
func fakeHome(t *testing.T) *App {
	t.Helper()
	return &App{home: tilde.Home(writeHome(t, twoFavourites))}
}

// cmdWithContext returns the command a completer is handed by cobra. The
// context is the part that matters: a completer that lists mounts runs a
// subprocess with it.
func cmdWithContext(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	return cmd
}

func TestCompleteTargets(t *testing.T) {
	t.Parallel()
	a := fakeHome(t)

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
			t.Parallel()
			got, directive := a.completeTargets(cmdWithContext(t), tc.args, tc.toComplete)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("completeTargets(%v, %q) = %v, want %v", tc.args, tc.toComplete, got, tc.want)
			}
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Errorf("directive = %v, want NoFileComp", directive)
			}
		})
	}
}

func TestCompleteMountsTakesOneArgument(t *testing.T) {
	t.Parallel()

	// No fake home: completeMounts reads the mount table, not the config.
	got, directive := completeMounts(cmdWithContext(t), []string{"already"}, "")
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
	t.Parallel()
	// A home with no ssh config, no favourites file and no config file, so
	// every underlying load either fails or comes back empty.
	a := &App{home: tilde.Home(t.TempDir())}

	completers := map[string]func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective){
		"completeTargets": a.completeTargets,
		"completeMounts":  completeMounts,
	}

	for name, complete := range completers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, args := range [][]string{nil, {"one"}, {"one", "two"}} {
				_, directive := complete(cmdWithContext(t), args, "")
				if directive&cobra.ShellCompDirectiveError != 0 {
					t.Errorf("%s(%v) returned ShellCompDirectiveError, want no filename fallback", name, args)
				}
			}
		})
	}
}
