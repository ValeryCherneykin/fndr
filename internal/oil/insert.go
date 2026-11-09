package oil

import (
	"slices"
	"unicode/utf8"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// Mode is what the keys do: run commands (Normal) or type text (Insert).
type Mode int

// Modes of the buffer.
const (
	Normal Mode = iota
	Insert
)

func (m Mode) String() string {
	if m == Insert {
		return "INSERT"
	}
	return "NORMAL"
}

// Mode returns the mode the buffer is in.
func (b *Buffer) Mode() Mode { return b.mode }

// In Insert mode the cursor sits between characters, so it may also be
// at the very end of the line, one past the last character, where typed
// text is appended.

// isInsert reports whether k is a command that starts Insert mode.
func isInsert(k string) bool {
	switch k {
	case "i", "a", "I", "A", "o", "O":
		return true
	}
	return false
}

// startInsert enters Insert mode where command k puts the cursor: before
// the cursor's character (i), after it (a), at the start (I) or end (A) of
// the line, or on a new line below (o) or above (O). A new line has no id:
// what is typed on it is created.
func (b *Buffer) startInsert(k string) {
	b.mode = Insert
	s := b.text()
	switch k {
	case "a":
		b.place(nextChar(s, b.col))
	case "I":
		b.place(firstNonBlank(s))
	case "A":
		b.place(len(s))
	case "o", "O":
		if k == "o" {
			b.row++
		}
		b.lines = slices.Insert(b.lines, b.row, mutation.Line{})
		b.place(0)
	}
}

// insertKey applies a key in Insert mode. A key naming a single character
// is typed; the rest are the editing keys of an input line.
func (b *Buffer) insertKey(k string) {
	s := b.text()
	switch k {
	case "esc", "ctrl+c":
		// Back to Normal, one character to the left, as in Vim: the
		// cursor leaves the gap it typed into and sits on a character.
		b.mode = Normal
		b.setCol(prevChar(s, b.col))
	case "backspace":
		if b.col > 0 {
			lo := prevChar(s, b.col)
			b.lines[b.row].Text = s[:lo] + s[b.col:]
			b.place(lo)
		} else if b.row > 0 {
			b.joinUp()
		}
	case "enter":
		b.split()
	case "left":
		b.place(prevChar(s, b.col))
	case "right":
		b.place(nextChar(s, b.col))
	case "space":
		b.typeText(" ")
	default:
		if utf8.RuneCountInString(k) == 1 {
			b.typeText(k)
		}
	}
}

// typeText inserts t at the cursor and moves the cursor past it.
func (b *Buffer) typeText(t string) {
	s := b.text()
	b.lines[b.row].Text = s[:b.col] + t + s[b.col:]
	b.place(b.col + len(t))
}

// split breaks the line at the cursor, as enter does in any editor. The
// line keeps its id and the text before the cursor; the text after it
// becomes a new line, which mutation.Diff reads as a new entry.
func (b *Buffer) split() {
	s := b.text()
	b.lines[b.row].Text = s[:b.col]
	b.lines = slices.Insert(b.lines, b.row+1, mutation.Line{Text: s[b.col:]})
	b.row++
	b.place(0)
}

// joinUp appends the cursor's line to the one above, as backspace at the
// start of a line does in Neovim. The joined line's id is gone with it, so
// its entry shows as deleted in the plan, where the user sees it before
// anything happens.
func (b *Buffer) joinUp() {
	above := b.lines[b.row-1].Text
	b.lines[b.row-1].Text = above + b.text()
	b.lines = slices.Delete(b.lines, b.row, b.row+1)
	b.row--
	b.place(len(above))
}

// place puts the cursor at col in Insert mode, where it may be at the end
// of the line, and makes it the column j and k aim for.
func (b *Buffer) place(col int) {
	b.col = col
	b.want = charIndex(b.text(), col)
}
