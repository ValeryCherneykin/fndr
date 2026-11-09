// Package oil is the editing engine of OIL mode: a small Vim that edits the
// lines of a folder, one name per line.
//
// It knows nothing of the disk or the screen. Keys go in as strings ("j",
// "esc", "ctrl+r"), the way the interface receives them; the lines come out
// as mutation.Line, for mutation.Diff to turn into a plan. So all of Vim
// here is tested with keys and lines alone, without a terminal or a file.
package oil

import (
	"slices"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// maxCount caps a typed count. Vim caps its counts too: "99999999999j"
// must not overflow into a negative move.
const maxCount = 99999

// Buffer is a folder being edited.
//
// It always holds at least one line, as Vim's buffers do: an empty folder
// is one blank line. The cursor then always has a line to be on, and no
// code has to ask whether there is one. A blank line means nothing to
// mutation.Diff, so it never turns into a change.
type Buffer struct {
	lines []mutation.Line
	row   int // the cursor's line, always within lines

	// count is the number typed before a command, as in "3j"; 0 when none
	// was typed. Every command but a digit uses it up.
	count int
}

// New returns a buffer holding lines, with the cursor on the first. The
// lines are copied: the buffer edits its own.
func New(lines []mutation.Line) *Buffer {
	b := &Buffer{lines: slices.Clone(lines)}
	if len(b.lines) == 0 {
		b.lines = []mutation.Line{{}}
	}
	return b
}

// Lines returns a copy of the lines as they are now, for mutation.Diff.
func (b *Buffer) Lines() []mutation.Line { return slices.Clone(b.lines) }

// Row returns the line the cursor is on, counting from 0.
func (b *Buffer) Row() int { return b.row }

// Key applies one key press. Keys the buffer does not know do nothing,
// except to drop a count typed before them, as in Vim.
func (b *Buffer) Key(k string) {
	// A count is digits; a leading "0" is not one, because in Vim "0" is
	// also a motion (to the start of the line).
	if d, ok := digit(k); ok && (d > 0 || b.count > 0) {
		b.count = min(b.count*10+d, maxCount)
		return
	}
	n := max(b.count, 1)
	b.count = 0

	switch k {
	case "j", "down":
		b.row = min(b.row+n, len(b.lines)-1)
	case "k", "up":
		b.row = max(b.row-n, 0)
	}
}

// digit returns the value of k if it is a single decimal digit.
func digit(k string) (int, bool) {
	if len(k) == 1 && '0' <= k[0] && k[0] <= '9' {
		return int(k[0] - '0'), true
	}
	return 0, false
}
