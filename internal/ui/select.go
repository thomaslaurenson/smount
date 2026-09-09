package ui

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
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

// maxLabelShare is the percentage of the row the label column may take.
//
// The column is sized from every item rather than the ones on screen, so that
// it holds still while the list scrolls. Sizing it to the longest name alone
// would then let one outlier pad every short name in the list, including on
// screens where that outlier is nowhere in view, so it is capped here and the
// few names past the cap are clipped instead.
const maxLabelShare = 35

// Item is one selectable row.
type Item struct {
	Label  string
	Detail string
}

// Select shows a filterable list on the output stream and returns the chosen
// index.
//
// The index is into the original slice. It reports ErrCancelled if the user
// backs out, and the context's error if the work was cancelled while the list
// was on screen. Either way the terminal is put back as it was found.
func (u *UI) Select(ctx context.Context, title string, items []Item) (int, error) {
	if len(items) == 0 {
		return -1, ErrNoItems
	}
	if !u.Interactive() {
		return -1, ErrNotTerminal
	}

	state, err := term.MakeRaw(u.fd)
	if err != nil {
		return -1, err
	}
	defer func() { _ = term.Restore(u.fd, state) }()

	s := &selector{
		title:   title,
		items:   items,
		out:     u.out,
		palette: u.palette,
	}
	s.measure(u.size)
	s.refilter()
	defer s.clear()

	// Stopped before the terminal is restored, so that no read of the shared
	// reader is still outstanding once this returns.
	keys := newKeySource(u.in)
	defer keys.close()

	for {
		s.redraw()
		k, ok := keys.next(ctx)
		if !ok {
			if err := ctx.Err(); err != nil {
				return -1, err
			}
			return -1, ErrCancelled
		}

		switch k.r {
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
			// waiting behind this rune means more of a sequence rather than a
			// lone keypress.
			if !k.more {
				return -1, ErrCancelled
			}
			s.escape(ctx, keys)
		default:
			if k.r >= ' ' && k.r != keyDelete {
				s.filter = append(s.filter, k.r)
				s.refilter()
			}
		}
	}
}

// key is one rune read from the terminal, with the reader's view of whether
// more input was already waiting behind it.
//
// more is recorded at the moment of the read, by the one goroutine that owns
// the reader, because asking the reader afterwards from here would race with it.
type key struct {
	r    rune
	more bool
}

// keySource serves runes from a reader one at a time, and only while it is
// being asked for them.
//
// The read has to happen off the calling goroutine, because a terminal read
// cannot be interrupted and Select still has to return when its context is
// cancelled. It must not be left outstanding once Select has returned, though:
// the reader is the one every other prompt shares, and stdin is handed to sshfs
// as well, so a read still waiting on it takes a keystroke meant for the
// confirmation prompt or a passphrase meant for ssh. Reading one rune per
// request, rather than reading ahead in a loop, is what bounds it: between
// requests the goroutine is parked on a channel rather than on the terminal.
type keySource struct {
	// req carries a request for one rune. It is unbuffered, so a read only
	// starts once next is committed to waiting for the answer.
	req chan struct{}

	// keys carries the answer to one request.
	keys chan key

	// stop tells the goroutine to finish rather than serve another request.
	stop chan struct{}

	// done is closed when the goroutine has finished, so next can tell an
	// exhausted reader from a slow one instead of blocking on a request
	// nothing will take.
	done chan struct{}

	once sync.Once
}

// newKeySource starts a reader over in. Call close when the prompt is finished
// with it.
func newKeySource(in *bufio.Reader) *keySource {
	s := &keySource{
		req:  make(chan struct{}),
		keys: make(chan key),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go s.run(in)
	return s
}

// run serves one rune per request until it is stopped or the reader fails.
func (s *keySource) run(in *bufio.Reader) {
	defer close(s.done)
	for {
		select {
		case <-s.stop:
			return
		case <-s.req:
		}

		r, _, err := in.ReadRune()
		if err != nil {
			return
		}

		// more is recorded here, by the one goroutine that owns the reader,
		// because asking the reader from the other side would race with it.
		select {
		case s.keys <- key{r: r, more: in.Buffered() > 0}:
		case <-s.stop:
			return
		}
	}
}

// close stops the reader.
//
// It is safe to call more than once, and after it returns the goroutine cannot
// begin another read. A read already in flight is one this prompt asked for and
// then abandoned, which only happens when the context was cancelled and the
// process is on its way out.
func (s *keySource) close() {
	s.once.Do(func() { close(s.stop) })
}

// next asks for the following key, reporting false when the reader has stopped
// or the context has been cancelled.
func (s *keySource) next(ctx context.Context) (key, bool) {
	select {
	case s.req <- struct{}{}:
	case <-s.done:
		return key{}, false
	case <-ctx.Done():
		return key{}, false
	}

	select {
	case k := <-s.keys:
		return k, true
	case <-s.done:
		return key{}, false
	case <-ctx.Done():
		return key{}, false
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

	// labelWidth is the column the labels are laid out in, fixed for the whole
	// prompt so that the detail beside them does not move while scrolling.
	labelWidth int

	out     io.Writer
	palette Palette
}

// measure fits the list to the terminal geometry it was handed.
func (s *selector) measure(size Size) {
	s.width = size.Width
	s.visible = maxVisible
	// Three lines of chrome plus one spare, so the list never pushes its own
	// title off the top of a short window.
	if room := size.Height - 4; room < s.visible {
		s.visible = room
	}
	if s.visible < 1 {
		s.visible = 1
	}
	s.labelWidth = labelColumn(s.items, s.width)
}

// labelColumn returns the width to lay the labels out in: the longest of them,
// capped to a share of the row where there are details to leave room for.
//
// The cap exists to stop one long label crowding out the column beside it, so
// with no detail on any item there is nothing to protect and the labels may
// have the whole row. A list of bare host aliases is the common case, and
// clipping a name there to keep space for nothing would be the worse fault.
func labelColumn(items []Item, cols int) int {
	longest, detailed := 0, false
	for _, item := range items {
		if n := width(item.Label); n > longest {
			longest = n
		}
		if item.Detail != "" {
			detailed = true
		}
	}
	if !detailed {
		return longest
	}

	limit := cols * maxLabelShare / 100
	if limit < 1 {
		limit = 1
	}
	if longest > limit {
		return limit
	}
	return longest
}

// escape consumes the remainder of an ANSI escape sequence and acts on the
// arrow keys, ignoring everything else.
func (s *selector) escape(ctx context.Context, keys *keySource) {
	k, ok := keys.next(ctx)
	if !ok {
		return
	}
	if k.r != '[' && k.r != 'O' {
		return
	}
	// A CSI sequence runs through its parameter bytes and ends at the first
	// byte in the range @ to ~, which is the one that says what it was.
	for {
		k, ok = keys.next(ctx)
		if !ok {
			return
		}
		if k.r >= '@' && k.r <= '~' {
			break
		}
	}
	switch k.r {
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
		if score, ok := itemScore(item, pattern); ok {
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

	end := s.offset + s.visible
	if end > len(s.matches) {
		end = len(s.matches)
	}

	for i := s.offset; i < end; i++ {
		item := s.items[s.matches[i]]
		marker := "  "
		if i == s.cursor {
			marker = "> "
		}
		out = append(out, marker+row(s.palette, item, s.labelWidth, s.width-len(marker)))
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
func row(p Palette, item Item, labelWidth, budget int) string {
	if budget < 1 {
		budget = 1
	}
	if labelWidth > budget {
		labelWidth = budget
	}
	// Clipped in the middle rather than at the end, because a set of aliases
	// sharing a long prefix is told apart only by its tails.
	label := middleTruncate(item.Label, labelWidth)
	if item.Detail == "" {
		return label
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
	return label + "  " + p.Dim(middleTruncate(item.Detail, rest))
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

// truncate clips s to cols columns, keeping the head and marking the cut with
// an ellipsis. The parameter is not named width, which is the function above.
//
// Keeping the head is right for the prose lines around the list, which read
// from the left and are still recognisable once cut. A host name goes through
// middleTruncate instead, since names differ at their ends.
func truncate(s string, cols int) string {
	if cols <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= cols {
		return s
	}
	if cols <= len(ellipsis) {
		return strings.Repeat(".", cols)
	}
	return string(r[:cols-len(ellipsis)]) + ellipsis
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

// detailPenalty puts every detail match below every label match.
//
// It only has to clear the worst score a label can produce, which is the
// subsequence band at 10000 plus the spread across the label.
const detailPenalty = 1_000_000

// itemScore scores pattern against one item, preferring its label.
//
// The detail is searched as well because it is on screen. A row reading
// "web01  deploy@10.0.0.15:2222" that cannot be found by typing the address
// sitting beside it makes the filter look broken.
//
// A detail match ranks below every label match rather than competing with one,
// so typing a name never buries the host it names under hosts that merely
// mention it.
func itemScore(item Item, pattern string) (int, bool) {
	if score, ok := match(item.Label, pattern); ok {
		return score, true
	}
	if score, ok := match(item.Detail, pattern); ok {
		return detailPenalty + score, true
	}
	return 0, false
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
