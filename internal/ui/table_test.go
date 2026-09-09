package ui

import (
	"strings"
	"testing"
)

func TestMiddleTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		cols  int
		want  string
	}{
		{name: "fits", input: "web01", cols: 10, want: "web01"},
		{name: "exact fit", input: "web01", cols: 5, want: "web01"},
		{name: "keeps both ends", input: "abcdefghij", cols: 9, want: "abc...hij"},
		{name: "odd budget favours the head", input: "abcdefghij", cols: 8, want: "abc...ij"},
		{name: "no room for content", input: "abcdefghij", cols: 3, want: "..."},
		{name: "less room than the ellipsis", input: "abcdefghij", cols: 2, want: ".."},
		{name: "no room at all", input: "web01", cols: 0, want: ""},
		{name: "negative", input: "web01", cols: -3, want: ""},
		// Counted in characters, so an accent costs one column and not two.
		{name: "multibyte counted by rune", input: "ééééééééé", cols: 7, want: "éé...éé"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := middleTruncate(tc.input, tc.cols)
			if got != tc.want {
				t.Errorf("middleTruncate(%q, %d) = %q, want %q", tc.input, tc.cols, got, tc.want)
			}
			if tc.cols > 0 && width(got) > tc.cols {
				t.Errorf("middleTruncate(%q, %d) = %q, which is %d columns", tc.input, tc.cols, got, width(got))
			}
		})
	}
}

// TestRenderTableFitsTheWidth is the guard for the whole point of the layout:
// one long value must not push any row past the terminal.
func TestRenderTableFitsTheWidth(t *testing.T) {
	t.Parallel()
	headers := []string{"HOST", "RESOLVES TO"}
	rows := [][]string{
		{"bioinformatics-pipeline-staging-server", ""},
		{"uoa-research-compute-node-01.its.auckland.ac.nz", "tlau083@uoa-research-compute-node-01.its.auckland.ac.nz"},
		{"web01", "deploy@10.0.0.15:2222"},
	}

	for _, cols := range []int{40, 60, 80, 120} {
		var b strings.Builder
		if err := RenderTable(&b, cols, headers, rows); err != nil {
			t.Fatalf("RenderTable() error = %v", err)
		}
		for _, line := range strings.Split(strings.TrimRight(b.String(), "\n"), "\n") {
			if width(line) > cols {
				t.Errorf("at %d columns the line %q is %d wide", cols, line, width(line))
			}
		}
	}
}

// TestRenderTableAtNaturalWidth covers the piped case, where clipping a value
// would corrupt it rather than tidy it.
func TestRenderTableAtNaturalWidth(t *testing.T) {
	t.Parallel()
	const long = "uoa-research-compute-node-01.its.auckland.ac.nz"
	var b strings.Builder

	if err := RenderTable(&b, 0, []string{"HOST", "RESOLVES TO"}, [][]string{{long, "web01"}}); err != nil {
		t.Fatalf("RenderTable() error = %v", err)
	}

	if !strings.Contains(b.String(), long) {
		t.Errorf("output = %q, want the full name %q in it", b.String(), long)
	}
	if strings.Contains(b.String(), ellipsis) {
		t.Errorf("output = %q, want nothing clipped", b.String())
	}
}

// TestRenderTableTrimsAnEmptyLastCell keeps a blank second column from padding
// every row out to the table width.
func TestRenderTableTrimsAnEmptyLastCell(t *testing.T) {
	t.Parallel()
	var b strings.Builder

	if err := RenderTable(&b, 80, []string{"HOST", "RESOLVES TO"}, [][]string{{"web01", ""}}); err != nil {
		t.Fatalf("RenderTable() error = %v", err)
	}

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if got := lines[len(lines)-1]; got != "web01" {
		t.Errorf("row = %q, want %q", got, "web01")
	}
}

// TestFitColumnsTakesFromTheWidest is the guard for a narrow column beside a
// wide one: the short column has nothing worth taking.
func TestFitColumnsTakesFromTheWidest(t *testing.T) {
	t.Parallel()
	widths := []int{47, 9}

	fitColumns(widths, 40)

	if widths[1] != 9 {
		t.Errorf("the narrow column shrank to %d, want it left at 9", widths[1])
	}
	if got := widths[0] + widths[1] + tableGutter; got > 40 {
		t.Errorf("total = %d, want it within 40", got)
	}
}

// TestFitColumnsStopsAtTheMinimum keeps the layout from reducing every column
// to punctuation when the terminal is far too narrow for the table.
func TestFitColumnsStopsAtTheMinimum(t *testing.T) {
	t.Parallel()
	widths := []int{47, 54}

	fitColumns(widths, 10)

	for i, w := range widths {
		if w < tableMinColumn {
			t.Errorf("column %d shrank to %d, below the minimum %d", i, w, tableMinColumn)
		}
	}
}

func TestRenderTableDropsAnEmptyColumn(t *testing.T) {
	t.Parallel()
	headers := []string{"NAME", "MOUNT POINT", "SOURCE", "STATUS"}
	tests := []struct {
		name     string
		rows     [][]string
		wantGone []string
		wantKept []string
	}{
		{
			name:     "nothing unusual leaves two columns",
			rows:     [][]string{{"web01", "", "web01", ""}, {"db", "", "db:/var", ""}},
			wantGone: []string{"MOUNT POINT", "STATUS"},
			wantKept: []string{"NAME", "SOURCE"},
		},
		{
			name:     "one odd row keeps its column for everyone",
			rows:     [][]string{{"web01", "", "web01", ""}, {"logs", "~/scratch/logs", "web01:/var/log", "stale"}},
			wantGone: nil,
			wantKept: []string{"NAME", "MOUNT POINT", "SOURCE", "STATUS"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			if err := RenderTable(&b, 80, headers, tc.rows); err != nil {
				t.Fatalf("RenderTable() error = %v", err)
			}
			header := strings.Split(b.String(), "\n")[0]
			for _, gone := range tc.wantGone {
				if strings.Contains(header, gone) {
					t.Errorf("header = %q, want %q dropped", header, gone)
				}
			}
			for _, kept := range tc.wantKept {
				if !strings.Contains(header, kept) {
					t.Errorf("header = %q, want %q kept", header, kept)
				}
			}
		})
	}
}

// TestRenderTableKeepsColumnsWithNoRows guards the degenerate cases: with
// nothing to judge a column by, dropping one would hide a heading the caller
// asked for.
func TestRenderTableKeepsColumnsWithNoRows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rows [][]string
	}{
		{name: "no rows at all", rows: nil},
		{name: "rows that are entirely empty", rows: [][]string{{"", ""}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			if err := RenderTable(&b, 80, []string{"NAME", "SOURCE"}, tc.rows); err != nil {
				t.Fatalf("RenderTable() error = %v", err)
			}
			header := strings.Split(b.String(), "\n")[0]
			for _, want := range []string{"NAME", "SOURCE"} {
				if !strings.Contains(header, want) {
					t.Errorf("header = %q, want %q in it", header, want)
				}
			}
		})
	}
}
