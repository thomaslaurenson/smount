package ui

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/term"
)

// Key codes read from a terminal in raw mode. In raw mode the driver stops
// translating these, so Ctrl-C arrives as a byte rather than a signal and has
// to be handled like any other key.
const (
	keyCtrlC      = 3
	keyCtrlD      = 4
	keyBackspaceH = 8
	keyEnter      = 13
	keyLineFeed   = 10
	keyCtrlN      = 14
	keyCtrlP      = 16
	keyCtrlU      = 21
	keyEscape     = 27
	keyDelete     = 127
)

// maxVisible caps how many matches are listed at once, so a long list does not
// scroll the rest of the terminal away.
const maxVisible = 10

// Item is one selectable row.
type Item struct {
	Label  string
	Detail string
}

// Select shows a filterable list on the output stream and returns the chosen
// index.
//
// The index is into the original slice. It reports ErrCancelled if the user
// backs out.
func (u *UI) Select(title string, items []Item) (int, error) {
	if len(items) == 0 {
		return -1, ErrNoItems
	}
	if !u.Interactive() {
		return -1, ErrNotTerminal
	}

	fd := u.fd
	state, err := term.MakeRaw(fd)
	if err != nil {
		return -1, err
	}
	restore := func() { _ = term.Restore(fd, state) }
	defer restore()
	defer restoreOnSignal(restore)()

	s := &selector{
		title: title,
		items: items,
		in:    u.in,
		out:   u.out,
	}
	s.measure(fd)
	s.refilter()
	defer s.clear()

	for {
		s.redraw()
		r, _, err := s.in.ReadRune()
		if err != nil {
			return -1, ErrCancelled
		}

		switch r {
		case keyCtrlC, keyCtrlD:
			return -1, ErrCancelled
		case keyEnter, keyLineFeed:
			if len(s.matches) == 0 {
				continue
			}
			return s.matches[s.cursor], nil
		case keyDelete, keyBackspaceH:
			if n := len(s.filter); n > 0 {
				s.filter = s.filter[:n-1]
				s.refilter()
			}
		case keyCtrlU:
			s.filter = nil
			s.refilter()
		case keyCtrlN:
			s.move(1)
		case keyCtrlP:
			s.move(-1)
		case keyEscape:
			// A bare Escape and the start of an arrow key sequence are the same
			// byte. Terminals emit a sequence as one burst, so anything already
			// buffered means more of a sequence rather than a lone keypress.
			if s.in.Buffered() == 0 {
				return -1, ErrCancelled
			}
			s.escape()
		default:
			if r >= ' ' && r != keyDelete {
				s.filter = append(s.filter, r)
				s.refilter()
			}
		}
	}
}

// signalExitBase is the offset a shell adds to a signal number when it reports
// what killed a process, so a program that exits on a signal itself reports the
// same status.
const signalExitBase = 128

// restoreOnSignal arranges for restore to run if the process is signalled, and
// returns the function that stops watching.
//
// A signal terminates the process without running deferred functions, so
// without this the shell is handed back a terminal still in raw mode with echo
// off, and the user has to type "stty sane" blind to recover it. Pressing
// Ctrl-C at the prompt is a different thing and already works: raw mode
// delivers it as a byte rather than a signal.
//
// It exits rather than re-raising the signal, because sending a signal to
// oneself is not portable and every target has to build. The exit status still
// says what happened.
func restoreOnSignal(restore func()) func() {
	received := make(chan os.Signal, 1)
	signal.Notify(received, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		sig, ok := <-received
		if !ok {
			return
		}
		restore()
		status := signalExitBase
		if number, ok := sig.(syscall.Signal); ok {
			status += int(number)
		}
		os.Exit(status)
	}()

	return func() {
		signal.Stop(received)
		close(received)
	}
}

// selector holds the state of one Select call.
type selector struct {
	title   string
	items   []Item
	matches []int
	filter  []rune
	cursor  int
	offset  int
	drawn   int
	width   int
	visible int
	in      *bufio.Reader
	out     io.Writer
}

// measure reads the terminal size, falling back to a conservative default when
// it cannot be determined, such as when stderr is a pipe.
func (s *selector) measure(fd int) {
	width, height, err := term.GetSize(fd)
	if err != nil || width <= 0 {
		width, height = 80, 24
	}
	s.width = width
	s.visible = maxVisible
	// Three lines of chrome plus one spare, so the list never pushes its own
	// title off the top of a short window.
	if room := height - 4; room < s.visible {
		s.visible = room
	}
	if s.visible < 1 {
		s.visible = 1
	}
}

// escape consumes the remainder of an ANSI escape sequence and acts on the
// arrow keys, ignoring everything else.
func (s *selector) escape() {
	r, _, err := s.in.ReadRune()
	if err != nil {
		return
	}
	if r != '[' && r != 'O' {
		return
	}
	// A CSI sequence runs through its parameter bytes and ends at the first
	// byte in the range @ to ~, which is the one that says what it was.
	for {
		r, _, err = s.in.ReadRune()
		if err != nil {
			return
		}
		if r >= '@' && r <= '~' {
			break
		}
	}
	switch r {
	case 'A':
		s.move(-1)
	case 'B':
		s.move(1)
	}
}

// move shifts the cursor and scrolls the viewport to keep it visible.
func (s *selector) move(delta int) {
	if len(s.matches) == 0 {
		return
	}
	s.cursor += delta
	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor > len(s.matches)-1 {
		s.cursor = len(s.matches) - 1
	}
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+s.visible {
		s.offset = s.cursor - s.visible + 1
	}
}

// refilter recomputes the match list, keeping the cursor at the top so the best
// match is selected as soon as the filter narrows.
func (s *selector) refilter() {
	pattern := string(s.filter)
	type scored struct {
		index int
		score int
	}

	var hits []scored
	for i, item := range s.items {
		if score, ok := match(item.Label, pattern); ok {
			hits = append(hits, scored{index: i, score: score})
		}
	}
	slices.SortStableFunc(hits, func(a, b scored) int { return cmp.Compare(a.score, b.score) })

	s.matches = s.matches[:0]
	for _, h := range hits {
		s.matches = append(s.matches, h.index)
	}
	s.cursor = 0
	s.offset = 0
}

// lines renders the current state as the exact rows to print.
func (s *selector) lines() []string {
	out := make([]string, 0, s.visible+3)
	out = append(out, truncate(s.title, s.width))
	out = append(out, truncate("> "+string(s.filter), s.width))

	labelWidth := 0
	end := s.offset + s.visible
	if end > len(s.matches) {
		end = len(s.matches)
	}
	for _, idx := range s.matches[s.offset:end] {
		if n := width(s.items[idx].Label); n > labelWidth {
			labelWidth = n
		}
	}

	for i := s.offset; i < end; i++ {
		item := s.items[s.matches[i]]
		marker := "  "
		if i == s.cursor {
			marker = "> "
		}
		out = append(out, marker+row(item, labelWidth, s.width-len(marker)))
	}

	if len(s.matches) == 0 {
		out = append(out, "  no match")
	}
	status := fmt.Sprintf("  %d/%d  up/down move, enter select, esc cancel",
		len(s.matches), len(s.items))
	if len(status) > s.width {
		// The counts are the part worth keeping when the window is narrow; the
		// key hints are only a reminder.
		status = fmt.Sprintf("  %d/%d", len(s.matches), len(s.items))
	}
	out = append(out, truncate(status, s.width))
	return out
}

// row renders one item, giving the label a fixed column so that details line up
// and clipping each part to the space actually available.
func row(item Item, labelWidth, budget int) string {
	if budget < 1 {
		budget = 1
	}
	label := truncate(item.Label, budget)
	if item.Detail == "" {
		return label
	}
	if labelWidth > budget {
		labelWidth = budget
	}
	if pad := labelWidth - width(label); pad > 0 {
		label += strings.Repeat(" ", pad)
	}

	rest := budget - width(label) - 2
	if rest < 4 {
		return label
	}
	// Dim is applied after clipping, so the escape bytes never count towards
	// the width and the column cannot drift.
	return label + "  \x1b[2m" + truncate(item.Detail, rest) + "\x1b[0m"
}

// width returns how many columns s occupies.
//
// Every other measurement here counts characters, so measuring a label in bytes
// would drift the detail column for anything outside ASCII. Favourite names and
// mount names are ASCII by construction, but a host alias is whatever the ssh
// config says, and hiding one to keep a column straight would be the wrong
// trade.
//
// This counts runes rather than display cells, so a full width character still
// occupies two columns and counts as one. Correcting for that needs an East
// Asian width table, which is a large dependency for something that does not
// appear in a host alias.
func width(s string) int {
	return utf8.RuneCountInString(s)
}

// truncate clips s to cols columns, marking a clipped string with a trailing
// tilde. The parameter is not named width, which is the function above.
func truncate(s string, cols int) string {
	if cols <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= cols {
		return s
	}
	if cols == 1 {
		return "~"
	}
	return string(r[:cols-1]) + "~"
}

// redraw repaints in place by moving back over the rows drawn last time.
func (s *selector) redraw() {
	var b strings.Builder
	if s.drawn > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", s.drawn)
	}

	lines := s.lines()
	for _, line := range lines {
		b.WriteString("\x1b[2K")
		b.WriteString(line)
		b.WriteString("\r\n")
	}
	// A shorter list than last time leaves rows below that have to be wiped,
	// then stepped back over so the next repaint starts in the right place.
	for i := len(lines); i < s.drawn; i++ {
		b.WriteString("\x1b[2K\r\n")
	}
	if extra := s.drawn - len(lines); extra > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", extra)
	}

	s.drawn = len(lines)
	fmt.Fprint(s.out, b.String())
}

// clear wipes the rendered list, leaving the terminal as it was found.
func (s *selector) clear() {
	if s.drawn == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[%dA", s.drawn)
	for i := 0; i < s.drawn; i++ {
		b.WriteString("\x1b[2K\r\n")
	}
	fmt.Fprintf(&b, "\x1b[%dA", s.drawn)
	s.drawn = 0
	fmt.Fprint(s.out, b.String())
}

// match scores pattern against text, reporting whether it matches at all.
// Lower scores rank higher: a prefix match beats a substring match, which beats
// a subsequence match.
func match(text, pattern string) (int, bool) {
	if pattern == "" {
		return 0, true
	}
	lowerText := strings.ToLower(text)
	lowerPattern := strings.ToLower(pattern)

	if strings.HasPrefix(lowerText, lowerPattern) {
		return 0, true
	}
	if i := strings.Index(lowerText, lowerPattern); i >= 0 {
		return 100 + i, true
	}
	if spread, ok := subsequence(lowerText, lowerPattern); ok {
		return 10000 + spread, true
	}
	return 0, false
}

// subsequence reports whether every rune of pattern appears in text in order,
// and how far apart the first and last matched runes ended up. A tight run of
// matches ranks above the same letters scattered across a longer name.
func subsequence(text, pattern string) (int, bool) {
	runes := []rune(text)
	want := []rune(pattern)
	if len(want) == 0 {
		return 0, true
	}

	first, last, at := -1, -1, 0
	for i, r := range runes {
		if r != want[at] {
			continue
		}
		if first < 0 {
			first = i
		}
		at++
		if at == len(want) {
			last = i
			break
		}
	}
	if at < len(want) {
		return 0, false
	}
	return last - first, true
}
