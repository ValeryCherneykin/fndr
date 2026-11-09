package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// Writing an OIL buffer takes three steps, and the disk is touched only
// in the last:
//
//  1. :w turns the buffer into a plan (mutation.Diff). A line that cannot
//     become a step stops it there, with the cursor put on that line.
//  2. The plan is shown, trashes in red, and nothing happens until y.
//  3. y carries it out off the UI goroutine (mutation.Apply), then the
//     folder is read again, so the buffer shows what is really on the
//     disk, whatever happened, and what was made is selected.

// oilWrittenMsg reports a plan carried out, fully or up to a failure.
type oilWrittenMsg struct {
	dir    string
	done   mutation.Plan // the steps that were carried out
	err    error         // why the next one was not
	quit   bool          // :wq: leave OIL once written
	remove []string      // for the index: paths gone or moved away
	add    []indexer.Entry
}

// oilWrite starts writing the buffer: it checks the edit and shows the
// plan, or says why there is none.
func (m *Model) oilWrite(quit bool) tea.Cmd {
	st := m.oil
	plan, err := mutation.Diff(st.items, st.buf.Lines())
	var lineErr *mutation.LineError
	switch {
	case errors.As(err, &lineErr):
		st.buf.GoTo(lineErr.Line - 1)
		m.setNotice(err.Error(), true)
	case err != nil:
		m.setNotice(err.Error(), true)
	case len(plan) == 0 && quit:
		return m.leaveOil("")
	case len(plan) == 0:
		m.setNotice("nothing to write", false)
	default:
		st.plan, st.quitAfter = plan, quit
	}
	return nil
}

// confirmKey is a key while the plan is shown: y carries it out; n, esc
// or q go back to the buffer, edits intact.
func (m *Model) confirmKey(msg tea.KeyPressMsg) tea.Cmd {
	st := m.oil
	if st.applying {
		return nil // already on its way: a second y must not apply it twice
	}
	switch msg.String() {
	case "y":
		st.applying = true
		return applyPlan(st.dir, st.plan, st.quitAfter, m.ix.Snapshot())
	case "n", "esc", "q":
		st.plan = nil
	}
	return nil
}

// applyPlan carries out plan in dir, off the UI goroutine, and works out
// what the index must forget and learn, so search finds the new names at
// once. snap is the index now, to tell what a create added.
func applyPlan(dir string, plan mutation.Plan, quit bool, snap []indexer.Entry) tea.Cmd {
	return func() tea.Msg {
		msg := oilWrittenMsg{dir: dir, quit: quit}
		trash, err := mutation.HomeTrash()
		if err != nil {
			msg.err = err // before anything is done: nothing half applied
			return msg
		}
		n, err := mutation.Apply(dir, plan, trash)
		msg.done, msg.err = plan[:n], err
		msg.remove, msg.add = indexChanges(dir, msg.done, snap)
		return msg
	}
}

// indexChanges returns what the index must drop and add after steps were
// carried out in dir. A renamed folder is read again below its new name:
// everything in it moved with it.
func indexChanges(dir string, done mutation.Plan, snap []indexer.Entry) (remove []string, add []indexer.Entry) {
	for _, op := range done {
		switch op.Kind {
		case mutation.OpDelete:
			remove = append(remove, filepath.Join(dir, op.From))
		case mutation.OpRename:
			remove = append(remove, filepath.Join(dir, op.From))
			add = append(add, tree(filepath.Join(dir, op.To))...)
		case mutation.OpCreate:
			path := filepath.Join(dir, op.To)
			first, _, _ := strings.Cut(filepath.ToSlash(filepath.Clean(op.To)), "/")
			add = append(add, newEntries(mutation.Created{Path: path, Top: filepath.Join(dir, first)}, snap)...)
		}
	}
	return remove, add
}

// tree returns the entry at path and, for a folder, everything below it.
func tree(path string) []indexer.Entry {
	info, err := os.Lstat(path)
	if err != nil {
		return nil
	}
	es := []indexer.Entry{indexer.NewEntry(path, info.Size(), info.ModTime().UnixNano(), info.IsDir())}
	if !info.IsDir() {
		return es
	}
	out := make(chan []indexer.Entry)
	go func() {
		defer close(out)
		_, _ = indexer.Crawl(context.Background(), path, out) //nolint:errcheck // unreadable parts are just not indexed
	}()
	for batch := range out {
		es = append(es, batch...)
	}
	return es
}

// oilWritten takes in a plan carried out: the index learns the changes,
// the status bar says what happened, and the folder is read again.
func (m *Model) oilWritten(msg *oilWrittenMsg) tea.Cmd {
	m.ix.Update(msg.remove, msg.add)
	st, reveal := m.oil, revealName(msg.done)
	if msg.err != nil {
		m.setNotice(fmt.Sprintf("%d of %d done: %v", len(msg.done), len(st.plan), msg.err), true)
	} else {
		m.setNotice(summary(msg.done), false)
	}

	st.plan, st.applying, st.wrote = nil, false, st.wrote || len(msg.done) > 0
	if msg.quit && msg.err == nil {
		return m.leaveOil(reveal)
	}
	// What was just made is always shown: a new ".env" turns hidden names
	// on, or it would vanish the moment it exists.
	hidden := st.hidden || strings.HasPrefix(reveal, ".")
	return loadOil(msg.dir, hidden, reveal)
}

// leaveOil ends OIL. If anything was written the folder is read again
// for the list, with reveal, a name in it, selected.
func (m *Model) leaveOil(reveal string) tea.Cmd {
	wrote := m.oil.wrote
	m.oil = nil
	if !wrote {
		return nil
	}
	if reveal != "" {
		reveal = filepath.Join(m.cwd, reveal)
	}
	return m.chdir(m.cwd, reveal)
}

// revealName is what to select once a plan is done: the first thing
// created or renamed, by its name in the folder ("bin/" for "bin/run.sh").
func revealName(done mutation.Plan) string {
	for _, op := range done {
		if op.Kind == mutation.OpCreate || op.Kind == mutation.OpRename {
			first, _, _ := strings.Cut(filepath.ToSlash(filepath.Clean(op.To)), "/")
			return first
		}
	}
	return ""
}

// summary says what a plan did: "renamed 1, created 2, trashed 1".
func summary(done mutation.Plan) string {
	var renamed, created, trashed int
	for _, op := range done {
		switch op.Kind {
		case mutation.OpRename:
			renamed++
		case mutation.OpCreate:
			created++
		case mutation.OpDelete:
			trashed++
		}
	}
	var parts []string
	for _, p := range []struct {
		verb string
		n    int
	}{{"renamed", renamed}, {"created", created}, {"trashed", trashed}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", p.verb, p.n))
		}
	}
	return strings.Join(parts, ", ")
}
