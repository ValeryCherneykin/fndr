package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

// Keys while searching or browsing. The query line takes text; chords
// move, enter, jump and quit. Other modes (naming, in create.go) have
// their own handler, so a key never means two things at once.

// handleKey is Update for a key press.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.setNotice("", false) // a notice describes the last action; any key moves on
	if m.naming {
		return m.handleNameKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		// Esc is always one step back: first the query, then the program.
		if m.query != "" {
			cmd := m.setQuery("")
			return m, cmd
		}
		return m.quit()
	case "ctrl+e":
		m.exitDir = m.jumpTarget()
		_ = m.learn(m.exitDir) // quitting: SaveHistory writes it once the interface has closed
		return m.quit()
	case "ctrl+v":
		// A switch, not a mode: every other key works the same with the pane
		// on or off.
		m.preview = !m.preview

	case "enter":
		cmd := m.activate()
		return m, cmd
	case "ctrl+o":
		m.startNaming()

	// Arrows left and right belong to the text, as in any input field; the
	// held-ctrl pair is the same with hands on the home row, h and l as in
	// Vim. Folders are entered with Enter and left with "-" or backspace.
	case "left", "ctrl+h":
		m.cursorLeft()
	case "right", "ctrl+l":
		m.cursorRight()

	case "up", "ctrl+p":
		m.move(-1)
	case "down", "ctrl+n":
		m.move(1)
	case "pgup":
		m.move(-m.listHeight())
	case "pgdown":
		m.move(m.listHeight())
	case "home":
		m.move(-len(m.results))
	case "end":
		m.move(len(m.results))

	case "backspace":
		if m.query == "" {
			// Nothing left to erase: backspace steps back out of the folder.
			cmd := m.up()
			return m, cmd
		}
		cmd := m.backspace()
		return m, cmd
	case "ctrl+w", "ctrl+u":
		cmd := m.editKey(msg.String())
		return m, cmd

	case "-":
		// As in Oil.nvim: "-" goes up a folder, but only with nothing typed;
		// in a query it is just a character ("vless-tun").
		if m.query == "" {
			cmd := m.up()
			return m, cmd
		}
		cmd := m.insert("-")
		return m, cmd

	default:
		// Printable input. Ctrl and Alt chords are commands, never text.
		if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			cmd := m.insert(msg.Text)
			return m, cmd
		}
	}
	return m, nil
}

// jumpTarget is where ctrl+e takes the shell.
//
// Browsing (empty query), the user is in cwd, whatever is selected: after
// going up, the selection rests on the folder just left, and jumping back
// into it would be a surprise. Searching, the user has found something:
// the selected folder, or the folder holding the selected file. Search,
// then jump: a zoxide that knows the whole disk.
func (m *Model) jumpTarget() string {
	if m.query == "" || len(m.results) == 0 {
		return m.cwd
	}
	e := &m.results[m.cursor].Entry
	if e.IsDir {
		return e.Path
	}
	return filepath.Dir(e.Path)
}

// move shifts the selection by delta rows, stopping at either end, and
// remembers the row as picked by the user.
func (m *Model) move(delta int) {
	if len(m.results) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.results)-1)
	m.picked = m.results[m.cursor].Entry.Path
	m.scrollToCursor()
}

// pickedRow is where the selection belongs in fresh results: on the row
// the user picked, if it is still among them, else on the same row number,
// clamped to the list.
func (m *Model) pickedRow() int {
	if m.picked != "" {
		for i := range m.results {
			if m.results[i].Entry.Path == m.picked {
				return i
			}
		}
	}
	return min(m.cursor, max(len(m.results)-1, 0))
}

// scrollToCursor adjusts the viewport so the selected row is visible, moving
// it as little as possible.
func (m *Model) scrollToCursor() {
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = min(max(m.offset, 0), max(len(m.results)-h, 0))
}

func (m *Model) listHeight() int {
	return max(m.height-chromeHeight, 1)
}
