package ui

import (
	"io"
	"strings"
)

// tableGutter is the blank space between two columns.
const tableGutter = 2

// tableMinColumn is the narrowest a column may be squeezed to. Past this a
// value is more ellipsis than content, so the layout stops shrinking and lets
// the row run over instead of printing something unreadable.
const tableMinColumn = 8

// ellipsis marks a value clipped to fit its column.
//
// Three dots rather than the tilde the menu uses, because a tilde is how every
// path smount prints names the home directory, so a clipped value ending in
// one reads as a path rather than as something cut short.
const ellipsis = "..."

// RenderTable writes headers and rows as aligned columns, fitted to a total
// width.
//
// The parameter is named total rather than width, which is the function that
// measures a string in this package.
//
// A total of zero lays the table out at its natural size and clips nothing,
// which is what a redirected or piped run wants: whatever is reading it is not
// a person looking at a window.
//
// A column that is empty on every row is dropped, header and all. Cells are
// left empty where a value would only repeat what another column already says,
// so a run where nothing is unusual would otherwise print a heading over a
// column of nothing and take that room from the columns that do differ.
func RenderTable(w io.Writer, total int, headers []string, rows [][]string) error {
	headers, rows = dropEmptyColumns(headers, rows)
	widths := naturalWidths(headers, rows)
	if total > 0 {
		fitColumns(widths, total)
	}

	var b strings.Builder
	b.WriteString(renderRow(headers, widths))
	b.WriteByte('\n')
	for _, row := range rows {
		b.WriteString(renderRow(row, widths))
		b.WriteByte('\n')
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// dropEmptyColumns removes every column that has no content on any row.
//
// A table with no rows keeps all its columns, since nothing there says a
// column is uninteresting rather than merely unused. A table whose every cell
// is empty keeps them too, rather than rendering as nothing at all.
func dropEmptyColumns(headers []string, rows [][]string) ([]string, [][]string) {
	if len(rows) == 0 {
		return headers, rows
	}

	keep := make([]int, 0, len(headers))
	for i := range headers {
		for _, row := range rows {
			if i < len(row) && row[i] != "" {
				keep = append(keep, i)
				break
			}
		}
	}
	if len(keep) == len(headers) || len(keep) == 0 {
		return headers, rows
	}

	kept := make([]string, 0, len(keep))
	for _, i := range keep {
		kept = append(kept, headers[i])
	}
	trimmed := make([][]string, 0, len(rows))
	for _, row := range rows {
		out := make([]string, 0, len(keep))
		for _, i := range keep {
			if i < len(row) {
				out = append(out, row[i])
				continue
			}
			out = append(out, "")
		}
		trimmed = append(trimmed, out)
	}
	return kept, trimmed
}

// naturalWidths returns the width each column needs to clip nothing.
func naturalWidths(headers []string, rows [][]string) []int {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = width(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && width(cell) > widths[i] {
				widths[i] = width(cell)
			}
		}
	}
	return widths
}

// fitColumns shrinks the widest column, one character at a time, until the row
// fits in total.
//
// Taking from the widest rather than from every column in proportion is what
// keeps a short column intact. A table whose columns are 47 and 5 characters
// wide has only one column worth taking space from, and proportional shrinking
// would clip the five character one for no gain.
func fitColumns(widths []int, total int) {
	budget := total - tableGutter*(len(widths)-1)
	if budget < 1 {
		return
	}
	for sum(widths) > budget {
		i := widestColumn(widths)
		if widths[i] <= tableMinColumn {
			return
		}
		widths[i]--
	}
}

func sum(widths []int) int {
	out := 0
	for _, w := range widths {
		out += w
	}
	return out
}

func widestColumn(widths []int) int {
	at := 0
	for i, w := range widths {
		if w > widths[at] {
			at = i
		}
	}
	return at
}

// renderRow lays one row out in the given columns.
//
// Trailing blanks are trimmed so that an empty final cell costs nothing: a row
// padded to the full table width is invisible on screen but shows up in
// anything that compares lines.
func renderRow(row []string, widths []int) string {
	var b strings.Builder
	for i, w := range widths {
		var cell string
		if i < len(row) {
			cell = middleTruncate(row[i], w)
		}
		if i > 0 {
			b.WriteString(strings.Repeat(" ", tableGutter))
		}
		b.WriteString(cell)
		if pad := w - width(cell); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// middleTruncate clips s to cols columns, keeping both ends.
//
// Both ends are kept because that is where host names differ. A set of aliases
// sharing a long prefix, or a set of fully qualified names sharing a domain,
// is told apart at one end or the other, so clipping either end alone leaves
// several rows looking identical.
func middleTruncate(s string, cols int) string {
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
	keep := cols - len(ellipsis)
	head := (keep + 1) / 2
	return string(r[:head]) + ellipsis + string(r[len(r)-(keep-head):])
}
