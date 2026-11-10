package tui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// textLine is a one-line editor: the query, and the path ctrl+r edits. pos
// is the text cursor, a byte offset into text, always on a rune boundary,
// so a Cyrillic or CJK character is stepped over and erased whole, never
// split in half.
//
// It is a plain value that knows nothing of searching: the query runs a
// search after each edit, the rename line does not, and both edit the same
// way because they share this code.
type textLine struct {
	text string
	pos  int
}

// key applies an editing key and reports whether k was one: the arrows
// and their held-ctrl pair (h and l, as in Vim), backspace, the shell's
// erase chords (ctrl+w the word before the cursor, ctrl+u everything
// before it), and printable text. Ctrl and Alt chords are never text.
func (l *textLine) key(msg tea.KeyPressMsg) bool {
	before, after := l.text[:l.pos], l.text[l.pos:]
	switch msg.String() {
	case "left", "ctrl+h":
		_, size := utf8.DecodeLastRuneInString(before)
		l.pos -= size // size is 0 at the start of the line
	case "right", "ctrl+l":
		_, size := utf8.DecodeRuneInString(after)
		l.pos += size // size is 0 at the end of the line
	case "backspace":
		_, size := utf8.DecodeLastRuneInString(before)
		l.set(before[:len(before)-size], after)
	case "ctrl+w":
		l.set(deleteLastWord(before), after)
	case "ctrl+u":
		l.set("", after)
	default:
		if msg.Text == "" || msg.Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
			return false
		}
		l.insert(msg.Text)
	}
	return true
}

// insert types s at the cursor.
func (l *textLine) insert(s string) {
	l.set(l.text[:l.pos]+s, l.text[l.pos:])
}

// set makes the line before+after with the cursor between them. Every
// edit goes through it, so no edit can leave the cursor out of place.
func (l *textLine) set(before, after string) {
	l.text, l.pos = before+after, len(before)
}

// queryLine is the query as a line to edit.
func (m *Model) queryLine() textLine { return textLine{m.query, m.qpos} }

// setQueryLine makes l the query, searching if its text changed.
func (m *Model) setQueryLine(l textLine) tea.Cmd {
	cmd := m.setQuery(l.text)
	m.qpos = l.pos
	return cmd
}

// deleteLastWord implements ctrl+w as in a shell: drop trailing spaces, then
// the word before them.
func deleteLastWord(s string) string {
	s = strings.TrimRight(s, " ")
	if i := strings.LastIndexByte(s, ' '); i >= 0 {
		return s[:i+1]
	}
	return ""
}
