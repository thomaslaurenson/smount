package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		pattern string
		want    bool
	}{
		{name: "empty pattern matches", text: "web01", pattern: "", want: true},
		{name: "prefix", text: "nectar_oc", pattern: "nect", want: true},
		{name: "substring", text: "nectar_oc", pattern: "tar", want: true},
		{name: "case insensitive", text: "NectarOC", pattern: "nectar", want: true},
		{name: "subsequence", text: "nectar_tunnel", pattern: "ntl", want: true},
		{name: "no match", text: "nectar_oc", pattern: "zzz", want: false},
		{name: "out of order is not a subsequence", text: "abc", pattern: "cb", want: false},
		{name: "pattern longer than text", text: "ab", pattern: "abc", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, got := match(tc.text, tc.pattern); got != tc.want {
				t.Errorf("match(%q, %q) matched = %v, want %v", tc.text, tc.pattern, got, tc.want)
			}
		})
	}
}

// TestMatchRanking pins the ordering the picker depends on: with eighty similar
// host names, the one that starts with what was typed has to come first.
func TestMatchRanking(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		better  string
		worse   string
		pattern string
	}{
		{name: "prefix beats substring", better: "nectar_oc", worse: "my_nectar", pattern: "nect"},
		{name: "substring beats subsequence", better: "my_nectar", worse: "n_e_c_t", pattern: "nect"},
		{name: "earlier substring wins", better: "a_nect", worse: "aaaa_nect", pattern: "nect"},
		{name: "tighter subsequence wins", better: "xnectx", worse: "n1e2c3t", pattern: "nect"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			betterScore, ok := match(tc.better, tc.pattern)
			if !ok {
				t.Fatalf("match(%q, %q) did not match", tc.better, tc.pattern)
			}
			worseScore, ok := match(tc.worse, tc.pattern)
			if !ok {
				t.Fatalf("match(%q, %q) did not match", tc.worse, tc.pattern)
			}
			if betterScore >= worseScore {
				t.Errorf("match(%q) = %d, want a lower score than match(%q) = %d",
					tc.better, betterScore, tc.worse, worseScore)
			}
		})
	}
}

func TestSubsequence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		pattern    string
		wantSpread int
		wantOK     bool
	}{
		{name: "adjacent", text: "abcd", pattern: "abc", wantSpread: 2, wantOK: true},
		{name: "spread out", text: "a1b2c", pattern: "abc", wantSpread: 4, wantOK: true},
		{name: "empty pattern", text: "abc", pattern: "", wantSpread: 0, wantOK: true},
		{name: "missing rune", text: "abc", pattern: "abd", wantOK: false},
		{name: "wrong order", text: "abc", pattern: "cba", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spread, ok := subsequence(tc.text, tc.pattern)
			if ok != tc.wantOK {
				t.Fatalf("subsequence(%q, %q) ok = %v, want %v", tc.text, tc.pattern, ok, tc.wantOK)
			}
			if ok && spread != tc.wantSpread {
				t.Errorf("subsequence(%q, %q) spread = %d, want %d",
					tc.text, tc.pattern, spread, tc.wantSpread)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		width int
		want  string
	}{
		{name: "fits", input: "web01", width: 10, want: "web01"},
		{name: "exact fit", input: "web01", width: 5, want: "web01"},
		{name: "clipped", input: "web01xyz", width: 5, want: "we..."},
		{name: "single column", input: "web01", width: 1, want: "."},
		{name: "no room", input: "web01", width: 0, want: ""},
		{name: "negative width", input: "web01", width: -3, want: ""},
		{name: "multibyte counted by rune", input: "aaaaa", width: 4, want: "a..."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := truncate(tc.input, tc.width); got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.width, got, tc.want)
			}
		})
	}
}

func TestRefilterOrdersAndResetsCursor(t *testing.T) {
	t.Parallel()
	s := &selector{
		items: []Item{
			{Label: "my_nectar"},
			{Label: "nectar_oc"},
			{Label: "unrelated"},
		},
		cursor: 2,
		offset: 2,
	}

	s.filter = []rune("nect")
	s.refilter()

	// Index 1 is the prefix match, so it has to sort ahead of the substring
	// match at index 0, and the unrelated entry has to drop out entirely.
	if want := []int{1, 0}; !reflect.DeepEqual(s.matches, want) {
		t.Errorf("matches = %v, want %v", s.matches, want)
	}
	if s.cursor != 0 || s.offset != 0 {
		t.Errorf("cursor, offset = %d, %d, want 0, 0", s.cursor, s.offset)
	}
}

func TestMoveClampsToTheMatchList(t *testing.T) {
	t.Parallel()
	s := &selector{
		items:   []Item{{Label: "a"}, {Label: "b"}, {Label: "c"}},
		visible: 2,
	}
	s.refilter()

	s.move(-1)
	if s.cursor != 0 {
		t.Errorf("cursor after moving up from the top = %d, want 0", s.cursor)
	}

	s.move(10)
	if s.cursor != 2 {
		t.Errorf("cursor after moving past the end = %d, want 2", s.cursor)
	}
	// A viewport of two rows showing the third item has to have scrolled by one.
	if s.offset != 1 {
		t.Errorf("offset = %d, want 1", s.offset)
	}
}

func TestMoveOnAnEmptyMatchList(t *testing.T) {
	t.Parallel()
	s := &selector{items: []Item{{Label: "a"}}, visible: 5}
	s.filter = []rune("zzz")
	s.refilter()

	s.move(1)

	if s.cursor != 0 {
		t.Errorf("cursor = %d, want 0", s.cursor)
	}
}

func TestLinesFitTheTerminalWidth(t *testing.T) {
	t.Parallel()
	s := &selector{
		title: "Select an SSH host",
		items: []Item{{Label: "a-very-long-host-name-indeed", Detail: "deploy@10.0.0.4:2222"}},
	}
	s.measure(Size{Width: 24, Height: 24})
	s.refilter()

	for _, line := range s.lines() {
		if len([]rune(stripANSI(line))) > s.width {
			t.Errorf("line %q is wider than the terminal width %d", line, s.width)
		}
	}
}

func TestWidthCountsCharactersNotBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "empty", input: "", want: 0},
		{name: "ascii", input: "web01", want: 5},
		{name: "accented letter counts once", input: "caf\u00e9", want: 4},
		{name: "non-latin script counts per character", input: "\u30b5\u30fc\u30d0", want: 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := width(tc.input); got != tc.want {
				t.Errorf("width(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// TestRowAlignsANonASCIILabel is the guard for measuring labels in characters.
// A host alias comes from the ssh config, so it is not ASCII by construction
// the way a favourite name is, and measuring one in bytes pushed its detail
// column out by a byte per accent.
func TestRowAlignsANonASCIILabel(t *testing.T) {
	t.Parallel()
	const labelWidth = 8

	const detail = "10.0.0.1"
	p := NewPalette(true)
	ascii := row(p, Item{Label: "webxx", Detail: detail}, labelWidth, 40)
	unicode := row(p, Item{Label: "w\u00e9bxx", Detail: detail}, labelWidth, 40)

	want := detailColumn(t, ascii, detail)
	if got := detailColumn(t, unicode, detail); got != want {
		t.Errorf("detail starts at column %d for a non-ASCII label, want %d as for the ASCII one", got, want)
	}
}

// detailColumn returns the character offset at which detail begins in a
// rendered row, which is the column the details of every row have to share.
func detailColumn(t *testing.T, rendered, detail string) int {
	t.Helper()
	plain := stripANSI(rendered)
	if !strings.HasSuffix(plain, detail) {
		t.Fatalf("rendered row %q does not end in the detail %q", plain, detail)
	}
	return len([]rune(plain)) - len([]rune(detail))
}

// stripANSI removes escape sequences so that a rendered line can be measured by
// the columns it actually occupies.
//
// A CSI sequence is ESC, then "[", then any number of parameter bytes, then a
// final byte in the range @ to ~. The scan has to step past that "[" before it
// starts looking for the terminator, because "[" is itself in that range: the
// obvious loop ends the sequence on its second byte and leaves the parameters
// behind as text, so "\x1b[2m" measures as the two columns "2m".
func stripANSI(s string) string {
	var out []rune
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '\x1b' {
			out = append(out, runes[i])
			continue
		}
		i++
		if i < len(runes) && (runes[i] == '[' || runes[i] == 'O') {
			i++
		}
		for i < len(runes) && (runes[i] < '@' || runes[i] > '~') {
			i++
		}
	}
	return string(out)
}

func TestStripANSI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain text", input: "web01", want: "web01"},
		{name: "dim", input: "\x1b[2mweb01\x1b[0m", want: "web01"},
		{name: "multi parameter", input: "\x1b[1;31mweb01\x1b[0m", want: "web01"},
		{name: "cursor up", input: "\x1b[12Aweb01", want: "web01"},
		{name: "clear line", input: "\x1b[2Kweb01", want: "web01"},
		{name: "escape alone", input: "\x1b", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := stripANSI(tc.input); got != tc.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestMeasureFitsTheList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		size        Size
		wantWidth   int
		wantVisible int
	}{
		{name: "a roomy window caps at maxVisible", size: Size{Width: 80, Height: 24}, wantWidth: 80, wantVisible: maxVisible},
		{name: "a short window leaves room for the chrome", size: Size{Width: 80, Height: 8}, wantWidth: 80, wantVisible: 4},
		// A window with no room left still has to draw one row, or there is
		// nothing to put the cursor on.
		{name: "a window with no room still shows one row", size: Size{Width: 40, Height: 4}, wantWidth: 40, wantVisible: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &selector{}

			s.measure(tc.size)

			if s.width != tc.wantWidth {
				t.Errorf("width = %d, want %d", s.width, tc.wantWidth)
			}
			if s.visible != tc.wantVisible {
				t.Errorf("visible = %d, want %d", s.visible, tc.wantVisible)
			}
		})
	}
}

// TestRowLeavesTheDetailPlainWithoutColour is the guard for a piped or
// NO_COLOR run: the detail is the only styled part of a row, so it is where a
// palette that was ignored would show up.
func TestRowLeavesTheDetailPlainWithoutColour(t *testing.T) {
	t.Parallel()
	item := Item{Label: "web01", Detail: "10.0.0.1"}

	got := row(NewPalette(false), item, 8, 40)

	if strings.Contains(got, "\x1b") {
		t.Errorf("row() = %q, want no escape sequences", got)
	}
	if want := "web01     10.0.0.1"; got != want {
		t.Errorf("row() = %q, want %q", got, want)
	}
}

// manyItems is a list long enough to scroll, with the one long label early
// enough that it leaves the viewport before the end.
func manyItems() []Item {
	items := []Item{
		{Label: "archived-box", Detail: "does not resolve"},
		{Label: "uoa-research-compute-node-01.its.auckland.ac.nz", Detail: "tlau083@uoa-research-compute-node-01"},
		{Label: "db-prod", Detail: "10.0.0.16"},
	}
	for i := 1; i <= 20; i++ {
		items = append(items, Item{
			Label:  fmt.Sprintf("node%02d", i),
			Detail: fmt.Sprintf("10.0.2.%d", i),
		})
	}
	return items
}

// TestLabelColumnHoldsStillWhileScrolling is the guard for sizing the column
// from every item rather than the visible ones. Sizing it from the window made
// the detail column jump sideways the moment the longest label scrolled out.
func TestLabelColumnHoldsStillWhileScrolling(t *testing.T) {
	t.Parallel()
	s := &selector{title: "Select an SSH host", items: manyItems()}
	s.measure(Size{Width: 80, Height: 24})
	s.refilter()

	at := func(offset int) int {
		s.offset = offset
		s.cursor = offset
		for _, line := range s.lines() {
			plain := stripANSI(line)
			if strings.HasPrefix(plain, "  node05 ") || strings.HasPrefix(plain, "> node05 ") {
				return strings.Index(plain, "10.0.2.5")
			}
		}
		t.Fatalf("node05 was not on screen at offset %d", offset)
		return -1
	}

	// node05 is visible in both windows, but the long label is only in the
	// first, which is exactly when the column used to move.
	if top, scrolled := at(0), at(6); top != scrolled {
		t.Errorf("the detail column starts at %d near the top and %d once scrolled, want them equal", top, scrolled)
	}
}

func TestLabelColumn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		items []Item
		cols  int
		want  int
	}{
		{
			name:  "the longest label when it is within the cap",
			items: []Item{{Label: "web01", Detail: "a"}, {Label: "db-prod", Detail: "b"}},
			cols:  80,
			want:  7,
		},
		{
			// 35 percent of 80. One long alias must not pad every short name
			// beside it out to its own width.
			name:  "capped at a share of the row",
			items: []Item{{Label: "web01", Detail: "a"}, {Label: strings.Repeat("x", 60), Detail: "b"}},
			cols:  80,
			want:  28,
		},
		{
			// With no detail anywhere there is nothing to keep room for, so a
			// long alias is not clipped to protect an empty column.
			name:  "no details means no cap",
			items: []Item{{Label: strings.Repeat("x", 60)}},
			cols:  80,
			want:  60,
		},
		{
			name:  "one detail is enough to bring the cap back",
			items: []Item{{Label: strings.Repeat("x", 60)}, {Label: "web01", Detail: "10.0.0.1"}},
			cols:  80,
			want:  28,
		},
		{
			name:  "never narrower than a single column",
			items: []Item{{Label: "web01", Detail: "d"}},
			cols:  1,
			want:  1,
		},
		{
			name:  "no items at all",
			items: nil,
			cols:  80,
			want:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := labelColumn(tc.items, tc.cols); got != tc.want {
				t.Errorf("labelColumn() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestRowClipsALabelInTheMiddle guards the half of the fix that keeps a long
// alias identifiable: aliases sharing a prefix differ only at their tails.
func TestRowClipsALabelInTheMiddle(t *testing.T) {
	t.Parallel()
	const label = "bioinformatics-pipeline-staging-server"

	got := stripANSI(row(NewPalette(false), Item{Label: label, Detail: "10.0.0.1"}, 20, 40))

	if !strings.HasPrefix(got, "bio") {
		t.Errorf("row() = %q, want the head kept", got)
	}
	if !strings.Contains(got, "server") {
		t.Errorf("row() = %q, want the tail kept", got)
	}
	if strings.Contains(got, "~") {
		t.Errorf("row() = %q, want no tilde, which means a home directory elsewhere", got)
	}
}
