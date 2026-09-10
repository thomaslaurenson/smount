package ui

import (
	"bufio"
	"context"
	"io"
	"os"
	"testing"
	"time"
)

// takeKey asks for one rune, failing rather than hanging when none arrives.
func takeKey(t *testing.T, s *keySource, what string) rune {
	t.Helper()

	type result struct {
		k  key
		ok bool
	}
	got := make(chan result, 1)
	go func() {
		k, ok := s.next(context.Background())
		got <- result{k, ok}
	}()

	select {
	case r := <-got:
		if !r.ok {
			t.Fatalf("%s: reader stopped before it answered", what)
		}
		return r.k.r
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: no key arrived, input went somewhere else", what)
		return 0
	}
}

// A prompt that has finished must leave nothing reading the shared stream. A
// reader still blocked on it takes the keystroke belonging to the next menu, to
// the confirmation prompt, or to the passphrase prompt ssh draws once sshfs
// starts.
func TestPromptsDoNotTakeEachOthersInput(t *testing.T) {
	t.Parallel()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pr.Close(); pw.Close() })

	in := bufio.NewReader(pr)

	// Two menus in a row, as choosing a host and then a path does.
	for _, want := range []rune{'a', 'b'} {
		keys := newKeySource(in)
		if _, err := io.WriteString(pw, string(want)); err != nil {
			t.Fatal(err)
		}
		if got := takeKey(t, keys, "menu"); got != want {
			t.Fatalf("menu read %q, want %q", got, want)
		}
		keys.close()
	}

	// Then a line, as the confirmation prompt reads one.
	if _, err := io.WriteString(pw, "yes\n"); err != nil {
		t.Fatal(err)
	}
	line := make(chan string, 1)
	go func() {
		l, _ := in.ReadString('\n')
		line <- l
	}()

	select {
	case got := <-line:
		if got != "yes\n" {
			t.Fatalf("confirmation read %q, want %q", got, "yes\n")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("confirmation prompt blocked, a finished menu still held the stream")
	}
}

// A closed source must not start another read, so that the descriptor is free
// for whatever smount hands it to next.
func TestClosedKeySourceLeavesTheStreamAlone(t *testing.T) {
	t.Parallel()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pr.Close(); pw.Close() })

	in := bufio.NewReader(pr)
	keys := newKeySource(in)
	if _, err := io.WriteString(pw, "x"); err != nil {
		t.Fatal(err)
	}
	takeKey(t, keys, "menu")
	keys.close()
	keys.close() // close is idempotent

	select {
	case <-keys.done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader still running after close")
	}

	if _, err := io.WriteString(pw, "hello\n"); err != nil {
		t.Fatal(err)
	}
	got, err := in.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello\n" {
		t.Fatalf("read %q after close, want %q", got, "hello\n")
	}
}

// A cancelled context has to end the wait even though the terminal read behind
// it cannot be interrupted.
func TestKeySourceNextHonoursCancellation(t *testing.T) {
	t.Parallel()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pr.Close(); pw.Close() })

	keys := newKeySource(bufio.NewReader(pr))
	defer keys.close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		_, ok := keys.next(ctx)
		done <- ok
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case ok := <-done:
		if ok {
			t.Fatal("next reported a key when it was cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("next ignored the cancelled context")
	}
}

// A reader that reaches EOF must report it rather than leave a prompt waiting
// on a request nothing will take.
func TestKeySourceReportsAClosedStream(t *testing.T) {
	t.Parallel()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pr.Close() })

	keys := newKeySource(bufio.NewReader(pr))
	defer keys.close()
	pw.Close()

	done := make(chan bool, 1)
	go func() {
		_, ok := keys.next(context.Background())
		done <- ok
	}()

	select {
	case ok := <-done:
		if ok {
			t.Fatal("next reported a key from a closed stream")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("next blocked on a closed stream")
	}
}
