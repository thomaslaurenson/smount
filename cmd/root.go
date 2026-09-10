// Package cmd wires smount's command line interface. Everything it does beyond
// argument parsing and printing lives under internal.
package cmd

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/mount"
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

	// colour is the --color flag. Its type refuses anything but a mode, so by
	// the time a command runs the value has already been checked.
	colour colourFlag

	// mounts reads the active sshfs mounts.
	//
	// It is a field rather than a direct call so that a test can hand the
	// command tree a table of its own. Nothing else can: mount.Active reads the
	// kernel's own mount table, and only a real sshfs mount appears in it.
	mounts func(context.Context) ([]mount.Mount, error)

	// tableWidth is what the tables on stdout fit themselves to, or zero when
	// stdout is not a terminal. It is asked of stdout rather than stderr
	// because a table is the answer, and the answer's stream is the one whose
	// width bounds it.
	tableWidth int
}

// colourFlag holds the --color mode and refuses anything else.
//
// Validating in the flag rather than in a pre-run hook is what makes every
// subcommand refuse a bad mode. version overrides the root's persistent hook,
// as the scaffolding calls for, so a check that lived there would be the one
// command that quietly accepted "--color beige".
type colourFlag struct{ mode ui.ColourMode }

// String returns the mode as typed, which is what the help text shows as the
// default.
func (c *colourFlag) String() string { return string(c.mode) }

// Set is called by the flag package while the command line is parsed.
func (c *colourFlag) Set(s string) error {
	mode, err := ui.ParseColourMode(s)
	if err != nil {
		return err
	}
	c.mode = mode
	return nil
}

// Type names the value in the usage line, so the help lists the modes rather
// than saying "string".
func (c *colourFlag) Type() string { return "auto|always|never" }

// buildUI settles what cmd knows about the streams and hands it to the UI.
//
// This runs from the root's PersistentPreRun rather than from NewRootCmd
// because --color is one of its inputs, and a flag has no value until cobra
// has parsed the command line.
//
// os.Stderr is asked directly because every question here is about the
// process: a prompt is drawn on the real error stream or not at all, whatever
// errw is wrapped in.
func (a *App) buildUI() {
	a.tableWidth = ui.TerminalWidth(os.Stdout)
	a.ui = ui.New(
		a.in,
		a.errw,
		ui.IsTerminal(a.in, os.Stderr),
		ui.TerminalSize(os.Stderr),
		ui.NewPalette(ui.ResolveColour(a.colour.mode, os.Getenv("NO_COLOR") != "", os.Stderr)),
	)
}

// loadConfig reads the settings, writing the defaults out when no file exists.
//
// The file is written so that there is something to edit. mount_base and the
// mount option baseline are worth changing, and a config a user has to invent
// from the README is one they never discover. Fields absent from the file keep
// their default on load, so writing it freezes only the keys it names.
//
// A write that fails warns rather than stops. Every command that loads the
// config runs perfectly well on the defaults, so a read-only home is a reason
// to say so once and carry on rather than to refuse the command.
//
// Completion deliberately does not come through here: it runs on every tab
// press and must leave nothing behind.
func (a *App) loadConfig() (*config.Config, error) {
	cfg, err := config.Load(a.home)
	if err != nil {
		return nil, err
	}
	if !config.Exists(a.home) {
		if err := config.Save(cfg); err != nil {
			a.ui.Warnf("could not write %s: %v", a.home.Collapse(config.Path(a.home)), err)
		}
	}
	return cfg, nil
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
	return newApp(home, in, errw).rootCmd(out)
}

// newApp settles the dependencies every subcommand shares.
func newApp(home string, in *os.File, errw io.Writer) *App {
	return &App{
		home:   tilde.Home(home),
		in:     in,
		errw:   errw,
		colour: colourFlag{mode: ui.ColourAuto},
		mounts: mount.Active,
	}
}

// rootCmd builds the command tree over a's dependencies, writing the answer to
// out.
func (a *App) rootCmd(out io.Writer) *cobra.Command {
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
		PersistentPreRun: func(*cobra.Command, []string) {
			a.buildUI()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runMount(cmd, opts, args)
		},
	}
	root.SetIn(a.in)
	root.SetOut(out)
	root.SetErr(a.errw)
	root.PersistentFlags().Var(&a.colour, "color",
		"when to colour output: auto, always or never")
	opts.register(root)

	root.AddCommand(
		a.newLsCmd(),
		a.newUmountCmd(),
		a.newHostsCmd(),
		a.newCheckCmd(),
		newVersionCmd(),
	)
	return root
}
