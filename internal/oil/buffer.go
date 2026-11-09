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
// the cursor would land, an operator only what to do with what lies
// between. A motion is linewise (j, G: whole lines) or charwise (l, $:
// part of the line), and the operator follows it.
//
// Keys are collected in pending until they make a command. A key that
// cannot continue what is pending ("dx") drops all of it, as in Vim.
package oil

import (
	"math"
	"slices"
	"strings"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// maxCount caps a count. Vim caps its counts too: "99999999999j" must not
// overflow into a negative move, nor "99999p" fill the memory.
const maxCount = 99999

// wantEnd is the column $ asks for: the end of every line j and k reach.
const wantEnd = math.MaxInt

// Buffer is a folder being edited.
//
// It always holds at least one line, as Vim's buffers do: an empty folder
// is one blank line. The cursor then always has a line to be on, and no
// code has to ask whether there is one. A blank line means nothing to
// mutation.Diff, so it never turns into a change.
type Buffer struct {
	lines []mutation.Line

	// The cursor. col is a byte offset into the line, always at the start
	// of a character and never past the last one (see text.go).
	row, col int

	// want is the column, in characters, that j and k aim for. Moving
	// down from column 10 through a short name and on to a long one comes
	// back to column 10, as in Vim. Horizontal moves set it.
	want int

	pend pending
	reg  register
}

// pending is the part of a command typed so far.
type pending struct {
	count   int    // digits typed since the last operator or command; 0 if none
	opCount int    // the count typed before the operator
	op      string // "d" or "y", waiting for its motion
	prefix  string // "g", waiting for the key that completes "gg"
}

// register holds what was last deleted or yanked, for p and P: whole
// lines, ids and all, or text from inside a line. Deleting a line and
// putting it back elsewhere keeps its id, so mutation.Diff sees the same
// entry: a move, not a delete and a create.
type register struct {
	linewise bool
	lines    []mutation.Line // linewise
	text     string          // charwise
}

// target is where a motion leads.
type target struct {
	row, col int
	linewise bool
	toEnd    bool // $: j and k then keep to the ends of lines
}

// aliases are commands Vim defines as an operator with a motion.
var aliases = map[string]struct{ op, motion string }{
	"x": {"d", "l"},
	"D": {"d", "$"},
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

// Col returns the cursor's byte offset into its line.
func (b *Buffer) Col() int { return b.col }

// text is the line the cursor is on.
func (b *Buffer) text() string { return b.lines[b.row].Text }

// Key applies one key press.
func (b *Buffer) Key(k string) {
	p := &b.pend

	// A count is digits; a leading "0" is not one, because "0" is also
	// a motion (to the start of the line).
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

	if a, ok := aliases[k]; ok && p.op == "" {
		p.op, p.opCount, p.count = a.op, p.count, 0
		k = a.motion
	}
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
		b.operateLines(cmd.op, b.row, min(b.row+n, len(b.lines))-1)
	case isMotion(k):
		t, ok := b.motion(k, n, counted)
		switch {
		case !ok: // the motion cannot move: nothing happens, as in Vim
		case cmd.op == "":
			b.moveTo(t)
		case t.linewise:
			b.operateLines(cmd.op, min(b.row, t.row), max(b.row, t.row))
		default:
			b.operateChars(cmd.op, min(b.col, t.col), max(b.col, t.col))
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
	case "j", "down", "k", "up", "gg", "G", "h", "left", "l", "right", "0", "^", "$":
		return true
	}
	return false
}

// motion returns where motion k leads, n times. ok is false when it
// cannot move at all, as j on the last line: Vim then does nothing, and an
// operator waiting for it does nothing either, rather than act on the
// line the cursor is on.
//
// A charwise target may be len(line), one past the last character: that
// is where "l" on the last character leads, so "dl" (x) deletes it. The
// cursor itself never goes there; moveTo stops it on the last character.
func (b *Buffer) motion(k string, n int, counted bool) (t target, ok bool) {
	last, s := len(b.lines)-1, b.text()
	t.row, t.col = b.row, b.col
	switch k {
	case "j", "down":
		t.row, t.linewise = min(b.row+n, last), true
		return t, b.row < last
	case "k", "up":
		t.row, t.linewise = max(b.row-n, 0), true
		return t, b.row > 0
	case "gg", "G": // to line n, counting from 1; without a count, the first or last
		t.linewise = true
		switch {
		case counted:
			t.row = min(n, len(b.lines)) - 1
		case k == "gg":
			t.row = 0
		default:
			t.row = last
		}
		return t, true
	case "h", "left":
		for range n {
			t.col = prevChar(s, t.col)
		}
		return t, t.col != b.col
	case "l", "right":
		for range n {
			t.col = nextChar(s, t.col)
		}
		return t, t.col != b.col
	case "0":
		t.col = 0
	case "^":
		t.col = firstNonBlank(s)
	case "$":
		t.col, t.toEnd = len(s), true
	}
	return t, true
}

// moveTo puts the cursor where a motion leads.
func (b *Buffer) moveTo(t target) {
	if t.linewise {
		b.row = t.row
		b.col = colOf(b.text(), b.want)
		return
	}
	b.setCol(t.col)
	if t.toEnd {
		b.want = wantEnd
	}
}

// setCol puts the cursor at col on its line, or on the last character if
// col is past it, and makes that the column j and k aim for.
func (b *Buffer) setCol(col int) {
	s := b.text()
	b.col = min(col, lastChar(s))
	b.want = charIndex(s, b.col)
}

// operateLines applies op to lines lo through hi. Both operators keep the
// lines in the register; d also removes them.
func (b *Buffer) operateLines(op string, lo, hi int) {
	b.reg = register{linewise: true, lines: slices.Clone(b.lines[lo : hi+1])}
	if op == "y" {
		b.row = lo // as in Vim: yanking upwards takes the cursor along
		b.col = colOf(b.text(), b.want)
		return
	}
	b.lines = slices.Delete(b.lines, lo, hi+1)
	if len(b.lines) == 0 {
		b.lines = []mutation.Line{{}}
	}
	b.row = min(lo, len(b.lines)-1)
	b.setCol(firstNonBlank(b.text()))
}

// operateChars applies op to the bytes lo up to hi of the cursor's line.
// Editing the text keeps the line's id: that is a rename.
func (b *Buffer) operateChars(op string, lo, hi int) {
	if lo == hi {
		return // "d0" at the start of a line: nothing to take, the register is kept
	}
	s := b.text()
	b.reg = register{text: s[lo:hi]}
	if op == "d" {
		b.lines[b.row].Text = s[:lo] + s[hi:]
	}
	b.setCol(lo)
}

// put inserts the register n times: whole lines below the cursor's line
// (p) or above it (P); text after the cursor's character (p) or before it
// (P). The cursor goes where Vim puts it: on the first line put, or the
// last character.
func (b *Buffer) put(below bool, n int) {
	if b.reg.linewise {
		b.putLines(below, n)
	} else if b.reg.text != "" {
		b.putText(below, n)
	}
}

func (b *Buffer) putLines(below bool, n int) {
	n = min(n, max(maxCount/len(b.reg.lines), 1))
	at := b.row
	if below {
		at++
	}
	put := make([]mutation.Line, 0, n*len(b.reg.lines))
	for range n {
		put = append(put, b.reg.lines...)
	}
	b.lines = slices.Insert(b.lines, at, put...)
	b.row = at
	b.setCol(firstNonBlank(b.text()))
}

func (b *Buffer) putText(below bool, n int) {
	n = min(n, max(maxCount/len(b.reg.text), 1))
	s, at := b.text(), b.col
	if below {
		at = nextChar(s, b.col)
	}
	put := strings.Repeat(b.reg.text, n)
	s = s[:at] + put + s[at:]
	b.lines[b.row].Text = s
	b.setCol(prevChar(s, at+len(put)))
}

// digit returns the value of k if it is a single decimal digit.
func digit(k string) (int, bool) {
	if len(k) == 1 && '0' <= k[0] && k[0] <= '9' {
		return int(k[0] - '0'), true
	}
	return 0, false
}
