// Package ui renders the interactive prompts smount falls back to when it is
// run without enough arguments to act on its own.
//
// Everything is drawn on stderr rather than stdout, so that the output of a
// command remains usable in a pipe even when smount had to ask a question to
// produce it.
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

// The streams every prompt reads and writes, held at package level so that a
// message from deep inside a command needs no writer threaded down to it.
var (
	// output is where prompts, messages and menus are drawn.
	output io.Writer = os.Stderr

	// input is the one buffered reader over standard input, shared by every
	// prompt.
	//
	// A bufio.Reader reads ahead, so a fresh one per prompt throws away
	// whatever the previous one buffered but had not yet returned. That is the
	// rest of a pasted or piped answer, and it disappears without a trace.
	input = bufio.NewReader(os.Stdin)

	// isTerminal reports whether smount can prompt at all.
	isTerminal = terminalAttached
)

// ResetForTesting restores the package streams to the real process streams.
//
// Call it at the start of any test that replaces them.
func ResetForTesting() {
	output = os.Stderr
	input = bufio.NewReader(os.Stdin)
	isTerminal = terminalAttached
}

// terminalAttached reports whether both ends of a conversation are a terminal:
// somewhere to read the answer from, and somewhere to draw the question.
func terminalAttached() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}

// Interactive reports whether smount is able to prompt.
func Interactive() bool {
	return isTerminal()
}

// Confirm asks a yes or no question, returning def when the answer is empty.
//
// This deliberately does not use raw mode. The terminal's own line editing is
// better than anything reimplemented here, and a mistyped answer to "are you
// sure" should be correctable.
func Confirm(question string, def bool) (bool, error) {
	suffix := "[y/N]"
	if def {
		suffix = "[Y/n]"
	}
	answer, err := Line(fmt.Sprintf("%s %s: ", question, suffix))
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
func Line(prompt string) (string, error) {
	if !Interactive() {
		return "", ErrNotTerminal
	}
	fmt.Fprint(output, prompt)
	line, err := input.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(output)
		return "", ErrCancelled
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Infof writes a progress message.
func Infof(format string, args ...any) {
	fmt.Fprintf(output, "[*] "+format+"\n", args...)
}

// Warnf writes a warning.
func Warnf(format string, args ...any) {
	fmt.Fprintf(output, "[!] "+format+"\n", args...)
}
