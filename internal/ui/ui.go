// Package ui renders what smount puts in front of a person: the prompts it
// falls back to when run without enough arguments to act on its own, and the
// markers, colour and table layout the rest of its output is written with.
package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Markers prefix every line smount writes for a person to read, saying which
// kind of line it is.
//
// A line carrying structured output takes none of them: a table or a list of
// names is the answer somebody asked for rather than a message about it, and a
// prefix there would have to be stripped by whatever reads it next.
const (
	// MarkInfo is information, progress, or a result.
	MarkInfo = "[*]"

	// MarkWarn is a warning or an error.
	MarkWarn = "[!]"

	// markQuestion is a question with an answer expected. It is unexported
	// because prompts are drawn here and nothing outside asks one.
	markQuestion = "[?]"
)

// Errors reported when a prompt cannot run or the user declines to answer.
var (
	ErrCancelled   = errors.New("cancelled")
	ErrNotTerminal = errors.New("stdin is not a terminal, so smount cannot prompt")
	ErrNoItems     = errors.New("nothing to choose from")
)

// UI draws prompts and messages on one pair of streams.
//
// It is built in cmd and passed down, so nothing here reaches for the process
// streams and a test can hand it its own answers and read back what it drew.
type UI struct {
	// in is the one buffered reader over the answer stream, shared by every
	// prompt.
	//
	// A bufio.Reader reads ahead, so a fresh one per prompt throws away
	// whatever the previous one buffered but had not yet returned. That is the
	// rest of a pasted or piped answer, and it disappears without a trace.
	in *bufio.Reader

	// out is where prompts, messages and menus are drawn.
	out io.Writer

	// fd is the descriptor behind in, which the filterable list puts into raw
	// mode. It is read only when the UI is interactive.
	fd int

	// size is the terminal geometry to draw within, measured once in cmd.
	size Size

	// palette is how, and whether, this UI's stream may be styled.
	palette Palette

	interactive bool
}

// New builds a UI that reads answers from in and draws on out.
//
// in is an *os.File rather than an io.Reader because the filterable list puts
// the terminal into raw mode, which needs a descriptor rather than a stream.
//
// interactive, size and palette are decided by the caller. All three are facts
// about the process, so they are settled once in cmd and handed down, rather
// than asked again here.
func New(in *os.File, out io.Writer, interactive bool, size Size, palette Palette) *UI {
	return &UI{
		in:          bufio.NewReader(in),
		out:         out,
		fd:          int(in.Fd()),
		size:        size.orDefault(),
		palette:     palette,
		interactive: interactive,
	}
}

// IsTerminal reports whether both ends of a conversation are a terminal:
// somewhere to read the answer from, and somewhere to draw the question.
func IsTerminal(in, out *os.File) bool {
	return term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}

// Default terminal geometry, used for a dimension that cannot be measured.
const (
	defaultWidth  = 80
	defaultHeight = 24
)

// Size is the terminal geometry that smount lays its output out within.
type Size struct {
	Width  int
	Height int
}

// TerminalSize returns the geometry of f, falling back to a conservative
// default for anything it cannot measure, such as when f is a pipe.
//
// Ask this of the stream smount draws on rather than the one it reads answers
// from. The two are normally the same terminal, but only the drawing end
// decides how much room a line has.
func TerminalSize(f *os.File) Size {
	width, height, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return Size{}.orDefault()
	}
	return Size{Width: width, Height: height}.orDefault()
}

// orDefault fills in a dimension that is missing or nonsensical, so that a UI
// always has room to lay something out in.
//
// The zero Size is what a caller building one by hand gets, and a width of
// zero would clip every line away to nothing rather than failing visibly.
func (s Size) orDefault() Size {
	if s.Width <= 0 {
		s.Width = defaultWidth
	}
	if s.Height <= 0 {
		s.Height = defaultHeight
	}
	return s
}

// TerminalWidth returns the width of f, or zero when f is not a terminal.
//
// Zero is how a table is told not to fit itself to anything. Output that is
// being piped or redirected keeps its full values, because what reads it is
// not a person with a window, and clipping a name there would corrupt it
// rather than tidy it.
func TerminalWidth(f *os.File) int {
	if !term.IsTerminal(int(f.Fd())) {
		return 0
	}
	return TerminalSize(f).Width
}

// ColourMode is when smount may write ANSI styling, as --color selects it.
type ColourMode string

// The values --color accepts.
const (
	ColourAuto   ColourMode = "auto"
	ColourAlways ColourMode = "always"
	ColourNever  ColourMode = "never"
)

// ErrColourMode is reported for a --color value that names no mode.
var ErrColourMode = errors.New("must be auto, always or never")

// ParseColourMode reads the value given to --color.
func ParseColourMode(s string) (ColourMode, error) {
	switch mode := ColourMode(s); mode {
	case ColourAuto, ColourAlways, ColourNever:
		return mode, nil
	default:
		return "", fmt.Errorf("%q: %w", s, ErrColourMode)
	}
}

// ResolveColour reports whether ANSI styling may be written to f.
//
// noColour says whether NO_COLOR is set, read in cmd because the environment
// belongs to the process rather than to this package.
//
// Only auto honours it. A mode the user typed is a decision about this run,
// and an environment variable set once in a shell profile should not overrule
// what was just asked for on the command line.
func ResolveColour(mode ColourMode, noColour bool, f *os.File) bool {
	switch mode {
	case ColourAlways:
		return true
	case ColourNever:
		return false
	default:
		return !noColour && term.IsTerminal(int(f.Fd()))
	}
}

// Palette renders text with ANSI styling, or plainly where colour is unwanted.
//
// The styles are deliberately muted. smount's output is read alongside
// whatever ssh and sshfs print, so a loud palette would make the decoration
// the thing the eye lands on rather than the host and path being described.
//
// The zero Palette writes nothing, which is the right answer for a stream
// nothing has said can show styling.
type Palette struct {
	enabled bool
}

// NewPalette builds a palette that styles text only when enabled.
func NewPalette(enabled bool) Palette {
	return Palette{enabled: enabled}
}

// Bold renders text worth picking out of a line, such as the characters a
// filter matched.
func (p Palette) Bold(s string) string {
	return p.wrap("1", s)
}

// Dim renders secondary text, such as the detail beside a menu label.
func (p Palette) Dim(s string) string {
	return p.wrap("2", s)
}

// wrap styles s, leaving it alone when colour is off or there is nothing to
// style. An empty string is returned bare so that a padded column never gains
// escape bytes around nothing.
func (p Palette) wrap(code, s string) string {
	if !p.enabled || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// Interactive reports whether smount is able to prompt.
func (u *UI) Interactive() bool {
	return u.interactive
}

// Confirm asks a yes or no question, returning def when the answer is empty.
//
// This deliberately does not use raw mode. The terminal's own line editing is
// better than anything reimplemented here, and a mistyped answer to "are you
// sure" should be correctable.
func (u *UI) Confirm(question string, def bool) (bool, error) {
	suffix := "[y/N]"
	if def {
		suffix = "[Y/n]"
	}
	answer, err := u.Line(fmt.Sprintf("%s %s: ", question, suffix))
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "":
		return def, nil
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// Line reads one line of input, with prompt shown on the output stream.
//
// The prompt is marked as a question, so that every line smount writes says
// which kind of line it is. Confirm asks through here too, so the marker is
// applied once for both.
//
// A final line with no newline after it is still an answer, so only a read that
// returned nothing at all counts as the user backing out.
func (u *UI) Line(prompt string) (string, error) {
	if !u.Interactive() {
		return "", ErrNotTerminal
	}
	fmt.Fprint(u.out, markQuestion+" "+prompt)
	line, err := u.in.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(u.out)
		return "", ErrCancelled
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Infof writes a progress message.
func (u *UI) Infof(format string, args ...any) {
	fmt.Fprintf(u.out, MarkInfo+" "+format+"\n", args...)
}

// Warnf writes a warning.
func (u *UI) Warnf(format string, args ...any) {
	fmt.Fprintf(u.out, MarkWarn+" "+format+"\n", args...)
}
