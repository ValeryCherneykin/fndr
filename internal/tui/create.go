package tui

import (
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// Creating is one chord and a name: ctrl+o turns the query line into a name
// field (NEW in the status bar), enter creates, esc cancels. The rules of
// the name are mutation.Create's: "notes.md" is a file, "drafts/" a folder,
// "a/b/c.txt" makes the folders on the way.
//
// What was made is always shown and selected: the query is cleared, the
// folder reread, and the cursor put on it. A file that is created and then
// hidden by the search the user happened to have typed looks like a file
// that was never created; that must never happen.

// createdMsg reports what Create did. added holds the new entries for the
// index, so search finds them at once, without waiting for a new crawl.
type createdMsg struct {
	c     mutation.Created
	added []indexer.Entry
	err   error
}

// startNaming turns the query line into an empty name field. The query
// stays as it was, under it, for esc to come back to.
func (m *Model) startNaming() {
	m.naming, m.name, m.npos = true, "", 0
}

// handleNameKey is handleKey while a name is typed. Only editing keys work:
// moving the list or entering folders would change where the name lands
// behind the user's back.
func (m *Model) handleNameKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		m.naming = false
	case "enter":
		return m, create(m.cwd, m.name, m.ix.Snapshot())
	case "left", "ctrl+h":
		m.cursorLeft()
	case "right", "ctrl+l":
		m.cursorRight()
	case "backspace":
		m.backspace() // at the start it erases nothing; it never leaves the folder
	case "ctrl+w", "ctrl+u":
		m.editKey(msg.String())
	default:
		if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			m.insert(msg.Text)
		}
	}
	return m, nil
}

// create makes name in dir off the UI goroutine: the disk may be slow (a
// network share, a sleeping drive), and the interface must not freeze.
// snap is the index as it is now, to tell what is new (see newEntries).
func create(dir, name string, snap []indexer.Entry) tea.Cmd {
	return func() tea.Msg {
		c, err := mutation.Create(dir, name)
		if err != nil {
			return createdMsg{err: err}
		}
		return createdMsg{c: c, added: newEntries(c, snap)}
	}
}

// newEntries reads what Create made, for the index: every level from Top
// down to Path that the index does not hold yet. "a/b/c.txt" may have made
// a, a/b and the file, or only the file if a/b was there; adding a twice
// would show it twice in search. Scanning the snapshot is linear, a few
// milliseconds for a million entries, and it runs here, off the UI.
func newEntries(c mutation.Created, snap []indexer.Entry) []indexer.Entry {
	fresh := map[string]bool{}
	for p := c.Path; ; p = filepath.Dir(p) {
		fresh[p] = true
		if p == c.Top || p == filepath.Dir(p) {
			break
		}
	}
	for i := range snap {
		delete(fresh, snap[i].Path)
	}
	es := make([]indexer.Entry, 0, len(fresh))
	for p := range fresh {
		if info, err := os.Lstat(p); err == nil {
			es = append(es, indexer.NewEntry(p, info.Size(), info.ModTime().UnixNano(), info.IsDir()))
		}
	}
	return es
}

// created shows the result of create. On error the name stays in the field,
// so a typo is fixed, not retyped. On success the line goes back to an empty
// query and the new entry is selected in the reread folder.
func (m *Model) created(msg *createdMsg) tea.Cmd {
	if msg.err != nil {
		m.setNotice(msg.err.Error(), true)
		return nil
	}
	m.ix.Update(nil, msg.added)
	m.naming = false
	m.setNotice("created "+m.tildify(msg.c.Path), false)
	return m.chdir(m.cwd, msg.c.Top)
}
