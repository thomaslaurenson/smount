package ui

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

// These tests replace the package streams, which is process wide state, so they
// do not call t.Parallel().

// withStreams points the package streams at in, captures what is written, and
// forces prompting on so a test with no terminal can still run one.
func withStreams(t *testing.T, in string) *bytes.Buffer {
	t.Helper()
	t.Cleanup(ResetForTesting)

	buf := &bytes.Buffer{}
	output = buf
	input = bufio.NewReader(strings.NewReader(in))
	isTerminal = func() bool { return true }
	return buf
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		def    bool
		want   bool
	}{
		{name: "empty takes a true default", answer: "\n", def: true, want: true},
		{name: "empty takes a false default", answer: "\n", def: false, want: false},
		{name: "y", answer: "y\n", want: true},
		{name: "yes", answer: "yes\n", want: true},
		{name: "uppercase Y", answer: "Y\n", want: true},
		{name: "uppercase YES", answer: "YES\n", want: true},
		{name: "surrounding whitespace is ignored", answer: "  y  \n", want: true},
		{name: "n", answer: "n\n", def: true, want: false},
		{name: "no", answer: "no\n", def: true, want: false},
		// Anything unrecognised is a no, including something that starts like a
		// yes. A confirmation is the wrong place to guess.
		{name: "an unrecognised answer is a no", answer: "yep\n", def: true, want: false},
		{name: "a bare newline after other text is a no", answer: "maybe\n", def: true, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withStreams(t, tc.answer)

			got, err := Confirm("Proceed?", tc.def)
			if err != nil {
				t.Fatalf("Confirm() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("Confirm(%q, def=%v) = %v, want %v", tc.answer, tc.def, got, tc.want)
			}
		})
	}
}

func TestConfirmShowsWhichAnswerIsTheDefault(t *testing.T) {
	tests := []struct {
		name string
		def  bool
		want string
	}{
		{name: "true default", def: true, want: "Proceed? [Y/n]: "},
		{name: "false default", def: false, want: "Proceed? [y/N]: "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := withStreams(t, "\n")

			if _, err := Confirm("Proceed?", tc.def); err != nil {
				t.Fatalf("Confirm() error = %v", err)
			}
			if got := buf.String(); got != tc.want {
				t.Errorf("prompt = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfirmCancelsOnNoInput(t *testing.T) {
	withStreams(t, "")

	if _, err := Confirm("Proceed?", true); !errors.Is(err, ErrCancelled) {
		t.Errorf("Confirm() on empty input error = %v, want %v", err, ErrCancelled)
	}
}

func TestLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "trailing newline is trimmed", input: "answer\n", want: "answer"},
		{name: "trailing carriage return is trimmed", input: "answer\r\n", want: "answer"},
		{name: "inner whitespace is kept", input: "  spaced out  \n", want: "  spaced out  "},
		{name: "an empty line is an empty answer", input: "\n", want: ""},
		// bufio.ReadString hands back the data it read alongside io.EOF, so a
		// last line with no newline after it is still an answer.
		{name: "a final line with no newline is still read", input: "answer", want: "answer"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withStreams(t, tc.input)

			got, err := Line("Remote path: ")
			if err != nil {
				t.Fatalf("Line() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("Line() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLineWritesThePrompt(t *testing.T) {
	buf := withStreams(t, "answer\n")

	if _, err := Line("Remote path: "); err != nil {
		t.Fatalf("Line() error = %v", err)
	}
	if got := buf.String(); got != "Remote path: " {
		t.Errorf("prompt = %q, want %q", got, "Remote path: ")
	}
}

func TestLineCancelsOnNoInput(t *testing.T) {
	withStreams(t, "")

	if _, err := Line("Remote path: "); !errors.Is(err, ErrCancelled) {
		t.Errorf("Line() on empty input error = %v, want %v", err, ErrCancelled)
	}
}

func TestLineRefusesWithoutATerminal(t *testing.T) {
	withStreams(t, "answer\n")
	isTerminal = func() bool { return false }

	if _, err := Line("Remote path: "); !errors.Is(err, ErrNotTerminal) {
		t.Errorf("Line() error = %v, want %v", err, ErrNotTerminal)
	}
}

// TestPromptsShareOneReader is the guard for the buffered reader. A reader
// created per prompt throws away whatever the previous one read ahead into its
// buffer, so the second question in a conversation would see nothing at all.
// The real sequence this protects is offerToSave: confirm, then name.
func TestPromptsShareOneReader(t *testing.T) {
	withStreams(t, "y\nlogs\nsecond\n")

	save, err := Confirm("Save this as a favourite?", false)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if !save {
		t.Fatal("Confirm() = false, want true")
	}

	name, err := Line("Favourite name: ")
	if err != nil {
		t.Fatalf("Line() error = %v", err)
	}
	if name != "logs" {
		t.Errorf("Line() = %q, want %q; the second prompt read the wrong line", name, "logs")
	}

	third, err := Line("Another: ")
	if err != nil {
		t.Fatalf("Line() error = %v", err)
	}
	if third != "second" {
		t.Errorf("Line() = %q, want %q", third, "second")
	}
}

func TestMessagePrefixes(t *testing.T) {
	tests := []struct {
		name  string
		write func(string, ...any)
		want  string
	}{
		{name: "info", write: Infof, want: "[*] mounted web01 at ~/sshfs/web01\n"},
		{name: "warning", write: Warnf, want: "[!] mounted web01 at ~/sshfs/web01\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := withStreams(t, "")

			tc.write("mounted %s at %s", "web01", "~/sshfs/web01")

			if got := buf.String(); got != tc.want {
				t.Errorf("output = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResetForTestingRestoresTheRealStreams(t *testing.T) {
	output = &bytes.Buffer{}
	isTerminal = func() bool { return true }

	ResetForTesting()

	if output == nil {
		t.Fatal("output = nil after ResetForTesting()")
	}
	if _, isBuffer := output.(*bytes.Buffer); isBuffer {
		t.Error("output is still the test buffer after ResetForTesting()")
	}
}
