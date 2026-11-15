package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

// Keys while searching or browsing. The query line takes text; chords
// move, enter, jump and quit. OIL (oil.go) has its own handler, so a key
// never means two things at once.

// handleKey is Update for a key press.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	m.setNotice("", false) // a notice describes the last action; any key moves on
	if m.leaving {         // the key after quit asked about the tasks (tasks.go), wherever it was asked
		return m.leaveKey(msg)
	}
	if m.oil != nil {
		return m.oilKey(msg)
	}
	if m.rename != nil {
		cmd := m.renameKey(msg)
		return cmd
	}
	if m.help != nil {
		return m.helpKey(msg)
	}
	if cmd, ok := m.bindKey(msg); ok {
		return cmd
	}
	if m.making != "" { // the key after ctrl+s offered a new folder (session.go)
		return m.makeKey(msg)
	}
	if m.undoing != nil { // the key after ctrl+z offered a batch (undo.go)
		return m.undoKey(msg)
	}
	if m.yanking { // the key after ctrl+y (copy.go)
		m.yanking = false
		cmd := m.copyKey(msg.Keystroke())
		return cmd
	}

	action := m.actionOf(msg)
	if do, ok := actions[action]; ok {
		if cmd, done := do(m); done {
			return cmd
		}
	}
	// With nothing typed, backspace steps back: it takes the last tag off,
	// as it erases the chip before the cursor, then goes out of the folder,
	// having nothing left to erase.
	if msg.String() == "backspace" && m.query == "" {
		if len(m.tags) > 0 {
			cmd := m.popTag()
			return cmd
		}
		cmd := m.up()
		return cmd
	}

	// The rest edits the query. The arrows left and right belong to the
	// text, as in any input field; folders are entered with Enter and left
	// with "-" or backspace.
	if l := m.queryLine(); l.key(action, msg) {
		cmd := m.setQueryLine(l)
		return cmd
	}
	return nil
}

// actionOf returns the action the user's keys give msg, or "". The key is
// looked up as Bubble Tea writes it ("ctrl+n", "J"), then as a keystroke
// ("shift+j", and on another keyboard layout the key's place, not its
// letter), the way the config names keys.
func (m *Model) actionOf(msg tea.KeyPressMsg) string {
	if a, ok := m.keys[msg.String()]; ok {
		return a
	}
	return m.keys[msg.Keystroke()]
}

// useKeys makes keys, the config's keys of every action, what the keys
// do: one table from key to action, for the key to look up.
func (m *Model) useKeys(keys map[string][]string) {
	m.actionKeys = keys // for the help page, in the file's order
	m.keys = make(map[string]string, len(keys))
	for action, ks := range keys {
		for _, k := range ks {
			m.keys[k] = action
		}
	}
}

// actionFunc does an action. done false lets the key go on to the query
// line: the action has nothing to do here ("-" with text typed is a dash),
// or the line does the rest (ctrl+u with no tags erases the text).
type actionFunc func(m *Model) (cmd tea.Cmd, done bool)

// lineActions are the actions the query line does itself (textLine.key);
// they need no entry in actions, but may have one.
var lineActions = []string{"cursor_left", "cursor_right", "delete_word", "clear"}

// actions are what the keys do while searching, by the config's names
// (config.Actions). A test sees that every name there is done here.
var actions = map[string]actionFunc{
	"down":      moveBy(func(*Model) int { return 1 }),
	"up":        moveBy(func(*Model) int { return -1 }),
	"page_down": moveBy(func(m *Model) int { return m.listHeight() }),
	"page_up":   moveBy(func(m *Model) int { return -m.listHeight() }),
	"first":     moveBy(func(m *Model) int { return -len(m.results) }),
	"last":      moveBy(func(m *Model) int { return len(m.results) }),

	"quit": func(m *Model) (tea.Cmd, bool) {
		return m.quit(), true
	},
	"back": func(m *Model) (tea.Cmd, bool) {
		// Esc is always one step back: the last tag, the query, then
		// GLOBAL, then fndr.
		switch {
		case len(m.tags) > 0:
			return m.popTag(), true
		case m.query != "":
			return m.setQuery(""), true
		case m.globalOn:
			return m.toggleGlobal(), true
		}
		return m.quit(), true
	},
	"global": func(m *Model) (tea.Cmd, bool) { return m.toggleGlobal(), true },
	"tag":    func(m *Model) (tea.Cmd, bool) { return m.addTag(), true },
	"clear": func(m *Model) (tea.Cmd, bool) {
		// It erases the line before the cursor; with nothing typed, what
		// is before it is the chips, all of them.
		if m.query == "" && len(m.tags) > 0 {
			m.tags = nil
			return m.tagsChanged(), true
		}
		return nil, false
	},
	"preview_down": func(m *Model) (tea.Cmd, bool) { m.scrollPane(1); return nil, true },
	"preview_up":   func(m *Model) (tea.Cmd, bool) { m.scrollPane(-1); return nil, true },
	"preview": func(m *Model) (tea.Cmd, bool) {
		// A switch, not a mode: every other key works the same with the
		// pane on or off.
		m.preview = !m.preview
		return nil, true
	},
	"reveal": func(m *Model) (tea.Cmd, bool) {
		// The selected entry, or, with nothing in the list, the folder.
		path, isDir := m.cwd, true
		if len(m.results) > 0 {
			e := &m.results[m.cursor].Entry
			path, isDir = e.Path, e.IsDir
		}
		return m.showInFiles(path, isDir), true
	},
	"copy_path": func(m *Model) (tea.Cmd, bool) {
		m.yanking = true
		m.setNotice(copyHint, false)
		return nil, true
	},
	"jump": func(m *Model) (tea.Cmd, bool) {
		m.exitDir = m.jumpTarget()
		_ = m.learn(m.exitDir) // its save never runs, quitting: SaveHistory writes it once the interface has closed
		return m.quit(), true
	},
	"open": func(m *Model) (tea.Cmd, bool) { return m.activate(), true },
	"oil": func(m *Model) (tea.Cmd, bool) {
		// Browsing, the folder; searching, what was found (oilsearch.go).
		if m.browsing() {
			return loadOil(m.cwd, false, ""), true
		}
		return m.oilFromSearch(), true
	},
	"rename":  func(m *Model) (tea.Cmd, bool) { m.startRename(); return nil, true },
	"ignored": func(m *Model) (tea.Cmd, bool) { return m.toggleWide(), true },
	"parent": func(m *Model) (tea.Cmd, bool) {
		// Up as in Oil.nvim, with nothing typed; in a query its key types
		// ("vless-tun").
		if m.query != "" {
			return nil, false
		}
		return m.up(), true
	},
	"help":    func(m *Model) (tea.Cmd, bool) { m.help = &helpState{}; return nil, true },
	"session": func(m *Model) (tea.Cmd, bool) { return m.openSession(), true },
	"undo":    func(m *Model) (tea.Cmd, bool) { return m.startUndo(), true },
}

// moveBy returns the action that moves the selection by delta(m) rows.
func moveBy(delta func(*Model) int) actionFunc {
	return func(m *Model) (tea.Cmd, bool) {
		m.move(delta(m))
		return nil, true
	}
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
