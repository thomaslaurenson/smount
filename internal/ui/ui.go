// Package ui renders the interactive prompts smount falls back to when it is
// run without enough arguments to act on its own.
//
// Everything is drawn on the error stream rather than stdout, so that the
// output of a command remains usable in a pipe even when smount had to ask a
// question to produce it.
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

	interactive bool
}

// New builds a UI that reads answers from in and draws on out.
//
// in is an *os.File rather than an io.Reader because the filterable list puts
// the terminal into raw mode, which needs a descriptor rather than a stream.
//
// interactive and size are decided by the caller. Both are facts about the
// process, so they are settled once in cmd and handed down, rather than asked
// again here.
func New(in *os.File, out io.Writer, interactive bool, size Size) *UI {
	return &UI{
		in:          bufio.NewReader(in),
		out:         out,
		fd:          int(in.Fd()),
		size:        size.orDefault(),
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
// A final line with no newline after it is still an answer, so only a read that
// returned nothing at all counts as the user backing out.
func (u *UI) Line(prompt string) (string, error) {
	if !u.Interactive() {
		return "", ErrNotTerminal
	}
	fmt.Fprint(u.out, prompt)
	line, err := u.in.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(u.out)
		return "", ErrCancelled
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Infof writes a progress message.
func (u *UI) Infof(format string, args ...any) {
	fmt.Fprintf(u.out, "[*] "+format+"\n", args...)
}

// Warnf writes a warning.
func (u *UI) Warnf(format string, args ...any) {
	fmt.Fprintf(u.out, "[!] "+format+"\n", args...)
}
