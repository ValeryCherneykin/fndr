// Package oil is the editing engine of OIL mode: a small Vim that edits the
// lines of a folder, one name per line.
//
// It knows nothing of the disk or the screen. Keys go in as strings ("j",
// "esc", "ctrl+r"), the way the interface receives them; the lines come out
// as mutation.Line, for mutation.Diff to turn into a plan. So all of Vim
// here is tested with keys and lines alone, without a terminal or a file.
//
// # Commands
//
// A Vim command is built from parts, each optional but the last:
//
//	[count] [operator] [count] motion
//
// "j" moves down; "dj" deletes the lines "j" would pass over; "yj" copies
// them; "2d3j" is "d6j", the counts multiplied. An operator typed twice
// works on lines from the cursor down: "dd", "3yy". So every operator works
// with every motion, and each is written once: a motion only says where
// the cursor would land, an operator only what to do with the lines
// between.
//
// Keys are collected in pending until they make a command. A key that
// cannot continue what is pending ("dx") drops all of it, as in Vim.
package oil

import (
	"slices"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// maxCount caps a count. Vim caps its counts too: "99999999999j" must not
// overflow into a negative move, nor "99999p" fill the memory.
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

	pend pending

	// reg holds the lines last deleted or yanked, ids and all, for p and
	// P. Deleting a line and putting it back elsewhere keeps its id, so
	// mutation.Diff sees the same entry: a move, not a delete and a create.
	reg []mutation.Line
}

// pending is the part of a command typed so far.
type pending struct {
	count   int    // digits typed since the last operator or command; 0 if none
	opCount int    // the count typed before the operator
	op      string // "d" or "y", waiting for its motion
	prefix  string // "g", waiting for the key that completes "gg"
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

// Key applies one key press.
func (b *Buffer) Key(k string) {
	p := &b.pend

	// A count is digits; a leading "0" is not one, because in Vim "0" is
	// also a motion (to the start of the line).
	if d, ok := digit(k); ok && p.prefix == "" && (d > 0 || p.count > 0) {
		p.count = min(p.count*10+d, maxCount)
		return
	}
	if k == "g" && p.prefix == "" {
		p.prefix = "g"
		return
	}
	k = p.prefix + k
	p.prefix = ""

	if p.op == "" && isOperator(k) {
		p.op, p.opCount, p.count = k, p.count, 0
		return
	}

	// From here on the key ends the command, whatever it does.
	cmd := *p
	b.pend = pending{}
	n, counted := cmd.total()

	switch {
	case cmd.op != "" && k == cmd.op: // "dd": n lines from the cursor down
		b.operate(cmd.op, b.row, min(b.row+n, len(b.lines))-1)
	case isMotion(k):
		to, ok := b.motion(k, n, counted)
		switch {
		case !ok: // the motion cannot move: nothing happens, as in Vim
		case cmd.op == "":
			b.row = to
		default:
			b.operate(cmd.op, min(b.row, to), max(b.row, to))
		}
	case cmd.op == "" && (k == "p" || k == "P"):
		b.put(k == "p", n)
	}
	// Anything else, "dx" or esc among them, has dropped what was pending.
}

// total returns the count of the command, both counts multiplied, and
// whether one was typed at all: "G" goes to the last line, "3G" to the
// third.
func (p *pending) total() (n int, counted bool) {
	n = min(max(p.opCount, 1)*max(p.count, 1), maxCount)
	return n, p.opCount > 0 || p.count > 0
}

func isOperator(k string) bool { return k == "d" || k == "y" }

func isMotion(k string) bool {
	switch k {
	case "j", "down", "k", "up", "gg", "G":
		return true
	}
	return false
}

// motion returns the line motion k moves the cursor to, n times. ok is
// false when it cannot move at all, as j on the last line: Vim then does
// nothing, and an operator waiting for it does nothing either, rather than
// act on the line the cursor is on.
func (b *Buffer) motion(k string, n int, counted bool) (to int, ok bool) {
	last := len(b.lines) - 1
	switch k {
	case "j", "down":
		return min(b.row+n, last), b.row < last
	case "k", "up":
		return max(b.row-n, 0), b.row > 0
	case "gg", "G": // to line n, counting from 1; without a count, the first or last
		switch {
		case counted:
			return min(n, len(b.lines)) - 1, true
		case k == "gg":
			return 0, true
		}
		return last, true
	}
	return b.row, false
}

// operate applies op to lines lo through hi. Both keep the lines in the
// register; d also removes them.
func (b *Buffer) operate(op string, lo, hi int) {
	b.reg = slices.Clone(b.lines[lo : hi+1])
	if op == "y" {
		b.row = lo // as in Vim: yanking upwards takes the cursor along
		return
	}
	b.lines = slices.Delete(b.lines, lo, hi+1)
	if len(b.lines) == 0 {
		b.lines = []mutation.Line{{}}
	}
	b.row = min(lo, len(b.lines)-1)
}

// put inserts the register n times below the cursor (p) or above it (P),
// and puts the cursor on the first line put.
func (b *Buffer) put(below bool, n int) {
	if len(b.reg) == 0 {
		return
	}
	n = min(n, max(maxCount/len(b.reg), 1))
	at := b.row
	if below {
		at++
	}
	put := make([]mutation.Line, 0, n*len(b.reg))
	for range n {
		put = append(put, b.reg...)
	}
	b.lines = slices.Insert(b.lines, at, put...)
	b.row = at
}

// digit returns the value of k if it is a single decimal digit.
func digit(k string) (int, bool) {
	if len(k) == 1 && '0' <= k[0] && k[0] <= '9' {
		return int(k[0] - '0'), true
	}
	return 0, false
}
