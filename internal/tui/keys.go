package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

// Keys while searching or browsing. The query line takes text; chords
// move, enter, jump and quit. OIL (oil.go) has its own handler, so a key
// never means two things at once.

// handleKey is Update for a key press.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.setNotice("", false) // a notice describes the last action; any key moves on
	if m.oil != nil {
		return m.oilKey(msg)
	}
	if m.rename != nil {
		cmd := m.renameKey(msg)
		return m, cmd
	}
	if cmd, ok := m.bindKey(msg); ok {
		return m, cmd
	}
	if m.yanking { // the key after ctrl+y (copy.go)
		m.yanking = false
		cmd := m.copyKey(msg.Keystroke())
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		// Esc is always one step back: the last tag, the query, then
		// GLOBAL, then fndr.
		if len(m.tags) > 0 {
			cmd := m.popTag()
			return m, cmd
		}
		if m.query != "" {
			cmd := m.setQuery("")
			return m, cmd
		}
		if m.globalOn {
			cmd := m.toggleGlobal()
			return m, cmd
		}
		return m.quit()
	case "tab":
		cmd := m.toggleGlobal()
		return m, cmd
	case "ctrl+t":
		cmd := m.addTag()
		return m, cmd
	case "ctrl+y":
		m.yanking = true
		m.setNotice(copyHint, false)
		return m, nil
	case "ctrl+u":
		// It erases the line before the cursor; with nothing typed, what is
		// before it is the chips, all of them. ctrl+l, once meant for this,
		// moves the cursor (as l in Vim).
		if m.query == "" && len(m.tags) > 0 {
			m.tags = nil
			cmd := m.tagsChanged()
			return m, cmd
		}
	case "ctrl+e":
		m.exitDir = m.jumpTarget()
		_ = m.learn(m.exitDir) // its save never runs, quitting: SaveHistory writes it once the interface has closed
		return m.quit()
	case "ctrl+v":
		// A switch, not a mode: every other key works the same with the pane
		// on or off.
		m.preview = !m.preview

	case "enter":
		cmd := m.activate()
		return m, cmd
	case "ctrl+o":
		// Browsing, the folder; searching, what was found (oilsearch.go).
		if m.browsing() {
			return m, loadOil(m.cwd, false, "")
		}
		cmd := m.oilFromSearch()
		return m, cmd
	case "ctrl+r":
		m.startRename()

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

	case "backspace", "-":
		// With nothing typed, both step back: backspace takes the last tag
		// off, as it erases the chip before the cursor, then out of the
		// folder, having nothing left to erase; "-" goes up as in
		// Oil.nvim. In a query they edit it ("vless-tun").
		if m.query == "" && len(m.tags) > 0 && msg.String() == "backspace" {
			cmd := m.popTag()
			return m, cmd
		}
		if m.query == "" {
			cmd := m.up()
			return m, cmd
		}
	}

	// The rest edits the query. The arrows left and right belong to the
	// text, as in any input field; folders are entered with Enter and left
	// with "-" or backspace.
	if l := m.queryLine(); l.key(msg) {
		cmd := m.setQueryLine(l)
		return m, cmd
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
	if m.browsing() || len(m.results) == 0 {
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
