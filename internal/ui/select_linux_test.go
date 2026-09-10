//go:build linux

package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// ioctl requests for allocating a pseudo terminal and setting its size. They
// are spelled out here rather than taken from golang.org/x/sys/unix, which the
// module only depends on indirectly: a terminal to drive Select against is not
// worth promoting a dependency for.
const (
	tiocGPTN   = 0x80045430
	tiocSPTLCK = 0x40045431
	tiocSWINSZ = 0x5414
)

// newPTY opens a pseudo terminal and returns both ends.
//
// Select puts its input into raw mode, which needs a real terminal: tcsetattr
// on a pipe fails, so the whole key dispatch loop is unreachable without one.
func newPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo terminals available: %v", err)
	}
	t.Cleanup(func() { master.Close() })

	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), tiocSPTLCK,
		uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Fatalf("unlocking the pty: %v", errno)
	}
	var number uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), tiocGPTN,
		uintptr(unsafe.Pointer(&number))); errno != 0 {
		t.Fatalf("finding the pty number: %v", errno)
	}

	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("opening the pty: %v", err)
	}
	t.Cleanup(func() { slave.Close() })

	// A fixed size, so the menu measures the same on any terminal the tests
	// happen to run under.
	size := struct{ rows, cols, x, y uint16 }{rows: 24, cols: 80}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, slave.Fd(), tiocSWINSZ,
		uintptr(unsafe.Pointer(&size))); errno != 0 {
		t.Fatalf("sizing the pty: %v", errno)
	}
	return master, slave
}

// selection is the outcome of one Select call running on its own goroutine.
type selection struct {
	index int
	err   error
	drawn string
}

// runSelect drives Select over a pseudo terminal, sending keys and returning
// what it chose.
//
// Keys are written as one burst per element, because Select tells a bare Escape
// from the start of an arrow key sequence by whether more input is already
// waiting behind it.
func runSelect(t *testing.T, ctx context.Context, items []Item, keys ...string) selection {
	t.Helper()
	master, slave := newPTY(t)

	var out strings.Builder
	u := New(slave, &out, true, TerminalSize(slave), NewPalette(true))

	done := make(chan selection, 1)
	go func() {
		idx, err := u.Select(ctx, "Pick one", items)
		done <- selection{index: idx, err: err}
	}()

	for _, k := range keys {
		// The menu has to have asked for input before the next burst arrives,
		// or two of them coalesce into one read and an escape sequence stops
		// looking like one.
		time.Sleep(20 * time.Millisecond)
		if _, err := master.WriteString(k); err != nil {
			t.Fatalf("writing %q: %v", k, err)
		}
	}

	select {
	case got := <-done:
		got.drawn = stripANSI(out.String())
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("Select did not return")
		return selection{}
	}
}

var threeItems = []Item{
	{Label: "web01", Detail: "10.0.0.1"},
	{Label: "web02", Detail: "10.0.0.2"},
	{Label: "database", Detail: "10.0.0.3"},
}

func TestSelectReturnsTheItemUnderTheCursor(t *testing.T) {
	t.Parallel()
	got := runSelect(t, t.Context(), threeItems, "\r")
	if got.err != nil {
		t.Fatalf("Select() error = %v", got.err)
	}
	if got.index != 0 {
		t.Errorf("Select() = %d, want the first item", got.index)
	}
	if !strings.Contains(got.drawn, "web01") {
		t.Errorf("drawn = %q, want the items listed", got.drawn)
	}
}

// The index is into the original slice, not into the filtered view, which is
// what makes a filtered choice point at the right item.
func TestSelectFiltersThenReturnsTheOriginalIndex(t *testing.T) {
	t.Parallel()
	got := runSelect(t, t.Context(), threeItems, "data", "\r")
	if got.err != nil {
		t.Fatalf("Select() error = %v", got.err)
	}
	if got.index != 2 {
		t.Errorf("Select() = %d, want the database entry at index 2", got.index)
	}
}

func TestSelectBackspaceWidensTheFilter(t *testing.T) {
	t.Parallel()
	// "webX" matches nothing, so the choice only succeeds once the X is gone.
	got := runSelect(t, t.Context(), threeItems, "webX", "\x7f", "\r")
	if got.err != nil {
		t.Fatalf("Select() error = %v", got.err)
	}
	if got.index != 0 {
		t.Errorf("Select() = %d, want the first match after backspace", got.index)
	}
}

func TestSelectMovesTheCursor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys []string
		want int
	}{
		{name: "down arrow", keys: []string{"\x1b[B", "\r"}, want: 1},
		{name: "down then up arrow", keys: []string{"\x1b[B", "\x1b[A", "\r"}, want: 0},
		{name: "ctrl-n", keys: []string{"\x0e", "\r"}, want: 1},
		{name: "ctrl-n twice then ctrl-p", keys: []string{"\x0e", "\x0e", "\x10", "\r"}, want: 1},
		{name: "down past the end clamps", keys: []string{"\x1b[B", "\x1b[B", "\x1b[B", "\x1b[B", "\r"}, want: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runSelect(t, t.Context(), threeItems, tc.keys...)
			if got.err != nil {
				t.Fatalf("Select() error = %v", got.err)
			}
			if got.index != tc.want {
				t.Errorf("Select() = %d, want %d", got.index, tc.want)
			}
		})
	}
}

func TestSelectCancels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
	}{
		{name: "ctrl-c", key: "\x03"},
		{name: "ctrl-d", key: "\x04"},
		{name: "a bare escape", key: "\x1b"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runSelect(t, t.Context(), threeItems, tc.key)
			if !errors.Is(got.err, ErrCancelled) {
				t.Errorf("Select() error = %v, want %v", got.err, ErrCancelled)
			}
			if got.index != -1 {
				t.Errorf("Select() = %d, want -1 on a cancelled prompt", got.index)
			}
		})
	}
}

// Enter with nothing matching has nothing to return, so the menu stays up
// rather than choosing something the filter excluded.
func TestSelectIgnoresEnterWithNoMatch(t *testing.T) {
	t.Parallel()
	got := runSelect(t, t.Context(), threeItems, "zzz", "\r", "\x7f\x7f\x7f", "\r")
	if got.err != nil {
		t.Fatalf("Select() error = %v", got.err)
	}
	if got.index != 0 {
		t.Errorf("Select() = %d, want the first item once the filter is cleared", got.index)
	}
	if !strings.Contains(got.drawn, "no match") {
		t.Errorf("drawn = %q, want it to say nothing matched", got.drawn)
	}
}

// A cancelled context has to end the menu even though nobody has pressed a key,
// because the work the menu is asking about has already stopped.
func TestSelectReturnsWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	got := runSelect(t, ctx, threeItems)
	if !errors.Is(got.err, context.Canceled) {
		t.Errorf("Select() error = %v, want %v", got.err, context.Canceled)
	}
}

func TestSelectRefusesWithNothingToChooseFrom(t *testing.T) {
	t.Parallel()
	_, slave := newPTY(t)
	u := New(slave, &strings.Builder{}, true, TerminalSize(slave), NewPalette(true))

	if _, err := u.Select(t.Context(), "Pick one", nil); !errors.Is(err, ErrNoItems) {
		t.Errorf("Select() error = %v, want %v", err, ErrNoItems)
	}
}

func TestSelectRefusesWithoutATerminal(t *testing.T) {
	t.Parallel()
	_, slave := newPTY(t)
	u := New(slave, &strings.Builder{}, false, TerminalSize(slave), NewPalette(true))

	if _, err := u.Select(t.Context(), "Pick one", threeItems); !errors.Is(err, ErrNotTerminal) {
		t.Errorf("Select() error = %v, want %v", err, ErrNotTerminal)
	}
}
