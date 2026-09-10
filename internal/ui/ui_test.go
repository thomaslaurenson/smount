package ui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newTestUI builds a UI that reads in as its answers and captures what it
// draws, with prompting forced on so a test with no terminal can still run one.
//
// The answers go through a real file because New takes one: the filterable list
// needs a descriptor to put into raw mode, which no in-memory reader has.
func newTestUI(t *testing.T, in string) (*UI, *bytes.Buffer) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "answers")
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatalf("writing answers: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening answers: %v", err)
	}
	t.Cleanup(func() { f.Close() })

	buf := &bytes.Buffer{}
	return New(f, buf, true, TerminalSize(f), NewPalette(false)), buf
}

func TestConfirm(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			u, _ := newTestUI(t, tc.answer)

			got, err := u.Confirm("Proceed?", tc.def)
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
	t.Parallel()
	tests := []struct {
		name string
		def  bool
		want string
	}{
		{name: "true default", def: true, want: "[?] Proceed? [Y/n]: "},
		{name: "false default", def: false, want: "[?] Proceed? [y/N]: "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u, buf := newTestUI(t, "\n")

			if _, err := u.Confirm("Proceed?", tc.def); err != nil {
				t.Fatalf("Confirm() error = %v", err)
			}
			if got := buf.String(); got != tc.want {
				t.Errorf("prompt = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfirmCancelsOnNoInput(t *testing.T) {
	t.Parallel()
	u, _ := newTestUI(t, "")

	if _, err := u.Confirm("Proceed?", true); !errors.Is(err, ErrCancelled) {
		t.Errorf("Confirm() on empty input error = %v, want %v", err, ErrCancelled)
	}
}

func TestLine(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			u, _ := newTestUI(t, tc.input)

			got, err := u.Line("Remote path: ")
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
	t.Parallel()
	u, buf := newTestUI(t, "answer\n")

	if _, err := u.Line("Remote path: "); err != nil {
		t.Fatalf("Line() error = %v", err)
	}
	if want := "[?] Remote path: "; buf.String() != want {
		t.Errorf("prompt = %q, want %q", buf.String(), want)
	}
}

func TestLineCancelsOnNoInput(t *testing.T) {
	t.Parallel()
	u, _ := newTestUI(t, "")

	if _, err := u.Line("Remote path: "); !errors.Is(err, ErrCancelled) {
		t.Errorf("Line() on empty input error = %v, want %v", err, ErrCancelled)
	}
}

func TestLineRefusesWithoutATerminal(t *testing.T) {
	t.Parallel()
	u, _ := newTestUI(t, "answer\n")
	u.interactive = false

	if _, err := u.Line("Remote path: "); !errors.Is(err, ErrNotTerminal) {
		t.Errorf("Line() error = %v, want %v", err, ErrNotTerminal)
	}
}

// TestPromptsShareOneReader is the guard for the buffered reader. A reader
// created per prompt throws away whatever the previous one read ahead into its
// buffer, so the second question in a conversation would see nothing at all.
// The real sequence this protects is offerToSave: confirm, then name.
func TestPromptsShareOneReader(t *testing.T) {
	t.Parallel()
	u, _ := newTestUI(t, "y\nlogs\nsecond\n")

	save, err := u.Confirm("Save this as a favourite?", false)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if !save {
		t.Fatal("Confirm() = false, want true")
	}

	name, err := u.Line("Favourite name: ")
	if err != nil {
		t.Fatalf("Line() error = %v", err)
	}
	if name != "logs" {
		t.Errorf("Line() = %q, want %q; the second prompt read the wrong line", name, "logs")
	}

	third, err := u.Line("Another: ")
	if err != nil {
		t.Fatalf("Line() error = %v", err)
	}
	if third != "second" {
		t.Errorf("Line() = %q, want %q", third, "second")
	}
}

func TestMessagePrefixes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(*UI, string, ...any)
		want  string
	}{
		{name: "info", write: (*UI).Infof, want: "[*] mounted web01 at ~/sshfs/web01\n"},
		{name: "warning", write: (*UI).Warnf, want: "[!] mounted web01 at ~/sshfs/web01\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u, buf := newTestUI(t, "")

			tc.write(u, "mounted %s at %s", "web01", "~/sshfs/web01")

			if got := buf.String(); got != tc.want {
				t.Errorf("output = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTerminalSizeFallsBackWithoutATerminal(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "not-a-terminal")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the file: %v", err)
	}
	t.Cleanup(func() { f.Close() })

	got := TerminalSize(f)

	if want := (Size{Width: defaultWidth, Height: defaultHeight}); got != want {
		t.Errorf("TerminalSize() = %+v, want %+v", got, want)
	}
}

func TestNewFillsInAnUnusableSize(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "answers")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("writing answers: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening answers: %v", err)
	}
	t.Cleanup(func() { f.Close() })

	u := New(f, &bytes.Buffer{}, true, Size{}, NewPalette(false))

	if want := (Size{Width: defaultWidth, Height: defaultHeight}); u.size != want {
		t.Errorf("size = %+v, want %+v", u.size, want)
	}
}

func TestParseColourMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    ColourMode
		wantErr bool
	}{
		{name: "auto", input: "auto", want: ColourAuto},
		{name: "always", input: "always", want: ColourAlways},
		{name: "never", input: "never", want: ColourNever},
		{name: "an unknown mode", input: "maybe", wantErr: true},
		{name: "the empty string", input: "", wantErr: true},
		// The values are matched exactly. Accepting "Always" here would make
		// the flag's own help text a partial description of what it takes.
		{name: "a mode in the wrong case", input: "Always", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseColourMode(tc.input)

			if tc.wantErr {
				if !errors.Is(err, ErrColourMode) {
					t.Fatalf("ParseColourMode(%q) error = %v, want %v", tc.input, err, ErrColourMode)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseColourMode(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseColourMode(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolveColour(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "not-a-terminal")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the file: %v", err)
	}
	t.Cleanup(func() { f.Close() })

	tests := []struct {
		name     string
		mode     ColourMode
		noColour bool
		want     bool
	}{
		// always and never are decisions about this run, so neither NO_COLOR
		// nor the stream being a file changes what they mean.
		{name: "always styles a file", mode: ColourAlways, want: true},
		{name: "always outranks NO_COLOR", mode: ColourAlways, noColour: true, want: true},
		{name: "never stays off", mode: ColourNever, want: false},
		{name: "auto declines a stream that is not a terminal", mode: ColourAuto, want: false},
		{name: "auto honours NO_COLOR", mode: ColourAuto, noColour: true, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveColour(tc.mode, tc.noColour, f); got != tc.want {
				t.Errorf("ResolveColour(%q, %v) = %v, want %v", tc.mode, tc.noColour, got, tc.want)
			}
		})
	}
}

func TestPaletteDim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enabled bool
		input   string
		want    string
	}{
		{name: "styled when enabled", enabled: true, input: "10.0.0.1", want: "\x1b[2m10.0.0.1\x1b[0m"},
		{name: "plain when disabled", input: "10.0.0.1", want: "10.0.0.1"},
		// Nothing to style means no escape bytes, so a padded column never
		// carries styling around an empty cell.
		{name: "empty text is left bare", enabled: true, input: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NewPalette(tc.enabled).Dim(tc.input); got != tc.want {
				t.Errorf("Dim(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestMarkersMatchTheConvention pins the vocabulary to the one every tool
// shares, so a rename here has to be a deliberate edit rather than a typo.
func TestMarkersMatchTheConvention(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		marker string
		want   string
	}{
		{name: "info", marker: MarkInfo, want: "[*]"},
		{name: "warning or error", marker: MarkWarn, want: "[!]"},
		{name: "question", marker: markQuestion, want: "[?]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.marker != tc.want {
				t.Errorf("marker = %q, want %q", tc.marker, tc.want)
			}
		})
	}
}
