package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// Row layout, in terminal cells:
//
//	▎ <icon> <name>     <dir>␠
//
// The marker column is always reserved, so rows do not shift sideways when
// the selection moves. The directory is shown relative to the search root and
// right-aligned, dim, so the eye goes to the name first.
const (
	rowFixed  = 5 // marker, space, icon, space, right padding
	rowMinGap = 2 // minimum spaces between name and directory
	ellipsis  = "…"
)

// renderRow renders one search result as exactly width cells.
//
// When space runs out the directory gives way first: it is cut from the left,
// keeping the part nearest the file ("…/cmd/fndr"), and dropped entirely when
// too little of it would remain to be useful. The name is cut, from the
// right, only when it alone does not fit.
//
// Matched characters are highlighted wherever they are, in the name or in the
// directory, so the user can see why a result matched.
//
// A habit row (a place the user often goes, shown above a folder's contents)
// carries a clock instead of its type icon: it says why the row is there.
func renderRow(r *indexer.Result, root string, width int, selected, habit bool) string {
	path := safeText(r.Entry.Path) // same length: offsets and positions still fit
	nameLo := len(path) - len(r.Entry.Name())
	dirLo, dirHi := relDir(path, nameLo, root)

	nameSt, dirSt := &stName, &stDir
	switch {
	case selected:
		nameSt, dirSt = &stNameSel, &stDirSel
	case strings.HasPrefix(r.Entry.Name(), "."):
		nameSt = &stNameHidden // there when looked for, never competing with the rest
	}

	avail := width - rowFixed
	if avail <= 0 {
		return strings.Repeat(" ", max(width, 0))
	}

	var b strings.Builder
	if selected {
		b.WriteString(stMarker.Render("▎"))
	} else {
		b.WriteByte(' ')
	}
	b.WriteByte(' ')
	if habit {
		b.WriteString(iconHabitStyled)
	} else {
		b.WriteString(iconFor(r.Entry.Name(), r.Entry.IsDir))
	}
	b.WriteByte(' ')

	nameW := ansi.StringWidth(path[nameLo:])
	if nameW > avail {
		// The name alone overflows: cut it and show no directory.
		hi := cutRight(path, nameLo, len(path), avail-1)
		b.WriteString(styleRange(path, nameLo, hi, r.Positions, nameSt, &stNameHit))
		b.WriteString(nameSt.Render(ellipsis))
		return finish(b.String(), width)
	}
	b.WriteString(styleRange(path, nameLo, len(path), r.Positions, nameSt, &stNameHit))

	// What is left for the gap and the directory.
	room := avail - nameW - rowMinGap
	dirW := ansi.StringWidth(path[dirLo:dirHi])
	var dir string
	switch {
	case dirLo >= dirHi || room < 6:
		// Nothing to show, or so little room that "…/x" would be noise.
	case dirW <= room:
		dir = styleRange(path, dirLo, dirHi, r.Positions, dirSt, &stDirHit)
	default:
		lo := cutLeft(path, dirLo, dirHi, room-1)
		dir = dirSt.Render(ellipsis) + styleRange(path, lo, dirHi, r.Positions, dirSt, &stDirHit)
	}

	gap := avail - nameW - lipgloss.Width(dir)
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(dir)
	return finish(b.String(), width)
}

// relDir returns the byte range of path holding its directory relative to
// root, without leading or trailing separators. The range is empty for
// entries directly in root.
func relDir(path string, nameLo int, root string) (lo, hi int) {
	hi = nameLo - 1 // drop the separator before the name
	switch {
	case root == "":
		lo = 0
	case strings.HasSuffix(root, string(filepath.Separator)):
		lo = len(root) // the filesystem root "/"
	case strings.HasPrefix(path, root):
		lo = len(root) + 1
	}
	return lo, max(hi, lo)
}

// styleRange renders path[lo:hi], drawing the bytes listed in pos (ascending
// byte offsets of matched characters) with hit and the rest with base.
// Consecutive characters of the same kind are rendered as one run, so a row
// costs a handful of style calls rather than one per character.
func styleRange(path string, lo, hi int, pos []int, base, hit *lipgloss.Style) string {
	k, _ := slices.BinarySearch(pos, lo) // first position at or after lo

	var b strings.Builder
	runLo, runHit := lo, false
	for i := lo; i < hi; {
		_, size := utf8.DecodeRuneInString(path[i:])
		isHit := k < len(pos) && pos[k] == i
		if isHit {
			k++
		}
		if i > runLo && isHit != runHit {
			b.WriteString(pick(runHit, hit, base).Render(path[runLo:i]))
			runLo = i
		}
		runHit = isHit
		i += size
	}
	if runLo < hi {
		b.WriteString(pick(runHit, hit, base).Render(path[runLo:hi]))
	}
	return b.String()
}

// pick returns a if cond, else b. Styles are passed by pointer throughout:
// a lipgloss.Style is over 600 bytes, and rendering copies none of them.
func pick(cond bool, a, b *lipgloss.Style) *lipgloss.Style {
	if cond {
		return a
	}
	return b
}

// cutRight returns the largest end such that s[lo:end] fits in cells cells,
// cutting only at rune boundaries.
func cutRight(s string, lo, hi, cells int) int {
	w := 0
	for i := lo; i < hi; {
		_, size := utf8.DecodeRuneInString(s[i:])
		rw := ansi.StringWidth(s[i : i+size])
		if w+rw > cells {
			return i
		}
		w += rw
		i += size
	}
	return hi
}

// cutLeft returns the smallest start such that s[start:hi] fits in cells
// cells, cutting only at rune boundaries.
func cutLeft(s string, lo, hi, cells int) int {
	w := 0
	for i := hi; i > lo; {
		_, size := utf8.DecodeLastRuneInString(s[lo:i])
		rw := ansi.StringWidth(s[i-size : i])
		if w+rw > cells {
			return i
		}
		w += rw
		i -= size
	}
	return lo
}

// finish pads or cuts a styled line to exactly width cells. Width math above
// is per rune, and a few exotic sequences (emoji joined with ZWJ) measure
// differently as a whole, so this is the guarantee that a row never wraps and
// breaks the layout.
func finish(line string, width int) string {
	if w := lipgloss.Width(line); w < width {
		return line + strings.Repeat(" ", width-w)
	} else if w > width {
		return ansi.Truncate(line, width, "")
	}
	return line
}
