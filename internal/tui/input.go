package tui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// The query line is a small line editor. m.qpos is the text cursor: a byte
// offset into m.query, always on a rune boundary, so a Cyrillic or CJK
// character is stepped over and erased whole, never split in half.
//
// Every edit goes through edit, which puts the cursor between the text
// before and after it, so no edit can leave the cursor out of place.
//
// The same editor serves the name of a new file while naming (create.go).
// line says which text the keys edit; only an edited query is searched.

// line returns the text the keys edit and its cursor: the name while
// naming, the query otherwise.
func (m *Model) line() (text *string, pos *int) {
	if m.naming {
		return &m.name, &m.npos
	}
	return &m.query, &m.qpos
}

// edit sets the line to before+after with the cursor between them.
func (m *Model) edit(before, after string) tea.Cmd {
	if m.naming {
		m.name, m.npos = before+after, len(before)
		return nil
	}
	cmd := m.setQuery(before + after)
	m.qpos = len(before)
	return cmd
}

// insert types s at the cursor.
func (m *Model) insert(s string) tea.Cmd {
	t, p := m.line()
	return m.edit((*t)[:*p]+s, (*t)[*p:])
}

// backspace erases the character before the cursor.
func (m *Model) backspace() tea.Cmd {
	t, p := m.line()
	_, size := utf8.DecodeLastRuneInString((*t)[:*p])
	return m.edit((*t)[:*p-size], (*t)[*p:])
}

// editKey applies the shell's erase chords: ctrl+w the word before the
// cursor, ctrl+u everything before it.
func (m *Model) editKey(k string) tea.Cmd {
	t, p := m.line()
	before := ""
	if k == "ctrl+w" {
		before = deleteLastWord((*t)[:*p])
	}
	return m.edit(before, (*t)[*p:])
}

// cursorLeft moves the cursor one character left, staying put at the start.
func (m *Model) cursorLeft() {
	t, p := m.line()
	_, size := utf8.DecodeLastRuneInString((*t)[:*p])
	*p -= size // size is 0 at the start of the line
}

// cursorRight moves the cursor one character right, staying put at the end.
func (m *Model) cursorRight() {
	t, p := m.line()
	_, size := utf8.DecodeRuneInString((*t)[*p:])
	*p += size // size is 0 at the end of the line
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
