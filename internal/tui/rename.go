package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// RENAME edits the path of what search found, in place of the query:
// ctrl+r, change the path, enter. A new name renames it; a new folder in
// the path moves it there, making the folders that are missing.
//
// The path is relative to fndr's folder, not to the entry's own: search
// finds things deep below, and that is where the path starts, so nothing
// ever needs "../". mutation.Diff refuses to leave the folder, and RENAME
// keeps that guard rather than working around it.
//
// Enter applies at once, without OIL's plan to confirm: the whole change
// is the one line on screen, nothing is deleted, and Apply never replaces
// what is already there.
//
// The edit is a one-line OIL buffer underneath: one item, one line with
// its id, through the same Diff and Apply, so both modes agree on what a
// name may be.

// renameState is a path being edited; m.rename is nil outside RENAME.
type renameState struct {
	from     string // the entry, absolute
	line     textLine
	applying bool // enter was pressed and the disk is being changed
}

// renamedMsg reports a rename carried out, or why it was not.
type renamedMsg struct {
	from, to string // absolute
	err      error
	remove   []string // for the index, as after an OIL write
	add      []indexer.Entry
}

// startRename enters RENAME for the selected entry.
func (m *Model) startRename() {
	if len(m.results) == 0 {
		m.setNotice("nothing selected to rename", false)
		return
	}
	from := m.results[m.cursor].Entry.Path
	rel, err := filepath.Rel(m.cwd, from)
	if err != nil || !filepath.IsLocal(rel) {
		// A frequent place on the empty query may lie anywhere.
		m.setNotice(m.tildify(from)+" is outside this folder: go there to rename it", true)
		return
	}
	rel = filepath.ToSlash(rel)
	m.rename = &renameState{from: from, line: textLine{rel, len(rel)}}
}

// renameKey is handleKey in RENAME: the keys edit the path, enter applies
// it, esc and ctrl+c leave it as it was.
func (m *Model) renameKey(msg tea.KeyPressMsg) tea.Cmd {
	st := m.rename
	if st.applying {
		return nil // on its way: a second enter must not apply it twice
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.rename = nil
	case "enter":
		return m.applyRename()
	default:
		st.line.key(msg)
	}
	return nil
}

// applyRename checks the edited path and starts the rename.
func (m *Model) applyRename() tea.Cmd {
	st := m.rename
	if strings.TrimSpace(st.line.text) == "" {
		// To Diff a blank line means "delete": never from a rename.
		m.setNotice("the new path is empty", true)
		return nil
	}
	rel, _ := filepath.Rel(m.cwd, st.from) // startRename checked it
	plan, err := mutation.Diff(
		[]mutation.Item{{ID: 1, Name: filepath.ToSlash(rel)}},
		[]mutation.Line{{ID: 1, Text: st.line.text}},
	)
	switch {
	case err != nil:
		var lineErr *mutation.LineError
		if errors.As(err, &lineErr) {
			err = lineErr.Err // there is one line: "line 1" says nothing
		}
		m.setNotice(err.Error(), true)
		return nil
	case len(plan) == 0:
		m.rename = nil // the same path: nothing to do
		return nil
	case len(plan) != 1 || plan[0].Kind != mutation.OpRename:
		// Cannot happen with one item and one line, unless the checks above
		// break: then refuse rather than hand Apply a delete with no trash.
		m.setNotice("not a rename", true)
		return nil
	}
	st.applying = true
	return renameEntry(m.cwd, plan[0], m.prune)
}

// renameEntry carries out op in dir, off the UI goroutine.
func renameEntry(dir string, op mutation.Op, prune *indexer.Prune) tea.Cmd {
	return func() tea.Msg {
		msg := renamedMsg{from: filepath.Join(dir, op.From), to: filepath.Join(dir, op.To)}
		plan := mutation.Plan{op}
		// No trash: a plan without deletes never asks for one.
		n, err := mutation.Apply(dir, plan, nil)
		msg.err = err
		msg.remove, msg.add = indexChanges(dir, plan[:n], nil, prune)
		return msg
	}
}

// renamed takes in a rename carried out. The query becomes the new name
// and the entry is selected, so it is in sight whatever the old query was:
// a renamed file must never vanish behind a filter.
func (m *Model) renamed(msg *renamedMsg) tea.Cmd {
	m.local.ix.Update(msg.remove, msg.add)
	if msg.err != nil {
		m.rename.applying = false // stay: the path is there to fix
		m.setNotice(msg.err.Error(), true)
		return nil
	}
	m.rename = nil
	verb, shown := "renamed", filepath.Base(msg.to)
	if filepath.Dir(msg.to) != filepath.Dir(msg.from) {
		rel, _ := filepath.Rel(m.cwd, msg.to) // the path as it was typed, from fndr's folder
		verb, shown = "moved", filepath.ToSlash(rel)
	}
	m.setNotice(fmt.Sprintf("%s %s → %s", verb, filepath.Base(msg.from), shown), false)

	cmd := m.setQuery(filepath.Base(msg.to))
	if cmd == nil {
		cmd = m.search() // the query was the name already: search again anyway, the index changed
	}
	m.qpos = len(m.query)
	m.picked = msg.to // after setQuery, which clears it
	return cmd
}
