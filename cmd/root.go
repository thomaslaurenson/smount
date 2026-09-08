// Package cmd wires smount's command line interface. Everything it does beyond
// argument parsing and printing lives under internal.
package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/tilde"
	"github.com/thomaslaurenson/smount/internal/ui"
)

const rootLong = `smount mounts remote directories over SSH using sshfs.

Run it with no arguments to choose a saved favourite or an SSH host
interactively, or name a target directly:

  smount web01               mount the remote home directory
  smount web01:/var/log      mount one remote path
  smount logs                mount the favourite named "logs"

Hosts come from ~/.ssh/config and every file it includes. Favourites and
settings live in ~/.smount.`

// reservedNames returns the names a favourite must not use. A bare
// "smount <name>" resolves a subcommand before it looks for a favourite, so a
// favourite sharing a name with one could never be mounted.
//
// The set is read off the command tree rather than listed by hand, because a
// hand-written list silently goes stale: it is the aliases that get forgotten,
// and an alias resolves exactly like the name it stands for. Only top level
// commands matter, since "smount bash" is not a command even though
// "smount completion bash" is.
func reservedNames(cmd *cobra.Command) map[string]bool {
	names := make(map[string]bool)
	for _, sub := range cmd.Root().Commands() {
		names[sub.Name()] = true
		for _, alias := range sub.Aliases {
			names[alias] = true
		}
	}
	return names
}

// ErrCancelled is reported when the user backs out of a prompt.
//
// It is re-exported from ui so that the entry point can recognise a cancelled
// prompt without reaching into internal for the sentinel.
var ErrCancelled = ui.ErrCancelled

// App holds the dependencies every subcommand shares.
type App struct {
	ui   *ui.UI
	home tilde.Home

	// in and errw are held so that the UI can be built once --color has been
	// read, which cobra only does when a command actually runs.
	in   *os.File
	errw io.Writer

	// colour is the --color flag, as typed.
	colour string
}

// buildUI settles what cmd knows about the streams and hands it to the UI.
//
// This runs from the root's PersistentPreRunE rather than from NewRootCmd
// because --color is one of its inputs, and a flag has no value until cobra
// has parsed the command line.
//
// os.Stderr is asked directly because every question here is about the
// process: a prompt is drawn on the real error stream or not at all, whatever
// errw is wrapped in.
func (a *App) buildUI() error {
	mode, err := ui.ParseColourMode(a.colour)
	if err != nil {
		return fmt.Errorf("--color %w", err)
	}
	a.ui = ui.New(
		a.in,
		a.errw,
		ui.IsTerminal(a.in, os.Stderr),
		ui.TerminalSize(os.Stderr),
		ui.NewPalette(ui.ResolveColour(mode, os.Getenv("NO_COLOR") != "", os.Stderr)),
	)
	return nil
}

// NewRootCmd builds the command tree, reading answers from in and writing
// output to out and errw.
//
// home is the directory every ~ path resolves against. It is read from the
// environment by the entry point and passed in, so nothing under internal has
// to look it up for itself.
//
// The streams are parameters rather than the process ones so that a test can
// build the same tree this binary does and read back what it wrote.
//
// What smount knows about the terminal, including whether it may prompt, is
// settled once in buildUI and handed to the UI from there.
func NewRootCmd(home string, in *os.File, out, errw io.Writer) *cobra.Command {
	a := &App{
		home: tilde.Home(home),
		in:   in,
		errw: errw,
	}
	opts := &mountOptions{}

	root := &cobra.Command{
		Use:               "smount [target]",
		Short:             "Mount remote directories over SSH with sshfs",
		Long:              rootLong,
		Args:              cobra.MaximumNArgs(1),
		SilenceErrors:     true,
		SilenceUsage:      true,
		Version:           Version,
		ValidArgsFunction: a.completeTargets,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return a.buildUI()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runMount(cmd, opts, args)
		},
	}
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errw)
	root.PersistentFlags().StringVar(&a.colour, "color", string(ui.ColourAuto),
		"when to colour output: auto, always or never")
	opts.register(root)

	root.AddCommand(
		newLsCmd(a.home),
		a.newUmountCmd(),
		a.newHostsCmd(),
		a.newCheckCmd(),
		newVersionCmd(),
	)
	return root
}
