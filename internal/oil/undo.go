package oil

import (
	"slices"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// Undo keeps whole copies of the lines, one per change, rather than a
// record of each edit and how to reverse it. A copy of the slice copies
// the line headers, not the names: 24 bytes a line, 24 KB a step for a
// folder of a thousand entries. Nothing has to know how to reverse
// itself, so nothing can reverse itself wrongly.
//
// A change is what one command does, as in Vim: "3dd" is one step, and so
// is all the typing from i to esc. A command that changes nothing leaves
// no step behind, so u never seems to do nothing.

// maxUndo bounds the steps kept; the oldest are forgotten first.
const maxUndo = 1000

// snapshot is the buffer as it was before a change.
type snapshot struct {
	lines    []mutation.Line
	row, col int
}

// Tick is a number that grows whenever the lines change, as Vim's
// b:changedtick does. It lets the interface skip work on keys that only
// move the cursor: the same tick, the same lines. It counts edits, not
// differences: typing a letter and deleting it moves it twice.
func (b *Buffer) Tick() uint64 { return b.tick }

// change records the buffer as it is, once per command, before the first
// edit the command makes. Every edit calls it; Key ends the command.
//
// The tick moves on every call, before the once-per-command check: all the
// typing from i to esc is one undo step but many edits, and each must be
// seen.
func (b *Buffer) change() {
	b.tick++
	if b.changing {
		return
	}
	b.changing = true
	b.undo = append(b.undo, b.snapshot())
	if len(b.undo) > maxUndo {
		b.undo = slices.Delete(b.undo, 0, len(b.undo)-maxUndo)
	}
	b.redo = nil // a new change ends the line of steps that U could go back to
}

func (b *Buffer) snapshot() snapshot {
	return snapshot{lines: slices.Clone(b.lines), row: b.row, col: b.col}
}

// undoN takes n steps back (u), or forward again (U, as redo is in
// fndr: ctrl+r rereads the folder, as in Oil.nvim).
func (b *Buffer) undoN(n int, back bool) {
	from, to := &b.undo, &b.redo
	if !back {
		from, to = to, from
	}
	for ; n > 0 && len(*from) > 0; n-- {
		*to = append(*to, b.snapshot())
		s := (*from)[len(*from)-1]
		*from = (*from)[:len(*from)-1]
		b.lines, b.row = s.lines, s.row
		b.setCol(s.col)
		b.tick++ // the lines changed without change(): it is for new edits only
	}
}
