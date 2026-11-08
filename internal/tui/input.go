package tui

import (
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// The query line is a small line editor. m.qpos is the text cursor: a byte
// offset into m.query, always on a rune boundary, so a Cyrillic or CJK
// character is stepped over and erased whole, never split in half.
//
// Every edit goes through edit, which puts the cursor between the text
// before and after it, so no edit can leave the cursor out of place.

// edit sets the query to before+after with the cursor between them.
func (m *Model) edit(before, after string) tea.Cmd {
	cmd := m.setQuery(before + after)
	m.qpos = len(before)
	return cmd
}

// insert types s at the cursor.
func (m *Model) insert(s string) tea.Cmd {
	return m.edit(m.query[:m.qpos]+s, m.query[m.qpos:])
}

// backspace erases the character before the cursor.
func (m *Model) backspace() tea.Cmd {
	_, size := utf8.DecodeLastRuneInString(m.query[:m.qpos])
	return m.edit(m.query[:m.qpos-size], m.query[m.qpos:])
}

// cursorLeft moves the cursor one character left, staying put at the start.
func (m *Model) cursorLeft() {
	_, size := utf8.DecodeLastRuneInString(m.query[:m.qpos])
	m.qpos -= size // size is 0 at the start of the query
}

// cursorRight moves the cursor one character right, staying put at the end.
func (m *Model) cursorRight() {
	_, size := utf8.DecodeRuneInString(m.query[m.qpos:])
	m.qpos += size // size is 0 at the end of the query
}
