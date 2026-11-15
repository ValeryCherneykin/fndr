package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// A copy under way is shown where it goes, before it is there: a ghost
// row in that folder's listing, its name dimmed, how far it got on the
// right where a row says its folder ("████░░░░░░  62%"). The result of an
// action is always in sight (a task done shows the real row, read from the
// disk); a ghost makes it so while the action runs too. Ghosts come right
// after the habits, above the folder's contents, in sight without
// scrolling. Only in the listing: in search, a ghost would have to be
// ranked against what is there.
//
// A ghost is a row like any other to move to; what it cannot do yet, being
// no file, it says (ghostRefuses): enter and ctrl+r. ctrl+x on one stops
// its copy.

// ghostBar is how many cells the progress bar takes.
const ghostBar = 10

// withGhosts returns listing with the ghosts of the folder's copies under
// way after its habits; listing itself if there are none.
func (m *Model) withGhosts(listing []indexer.Result) []indexer.Result {
	var ghosts []indexer.Result
	for _, t := range m.tasks {
		for _, to := range t.targets() {
			if filepath.Dir(to.path) == m.cwd {
				ghosts = append(ghosts, indexer.Result{Entry: indexer.NewEntry(to.path, 0, 0, to.dir)})
			}
		}
	}
	if len(ghosts) == 0 {
		return listing
	}
	out := make([]indexer.Result, 0, len(listing)+len(ghosts))
	out = append(out, listing[:m.habits]...)
	out = append(out, ghosts...)
	return append(out, listing[m.habits:]...)
}

// target is where a copy of a task goes, and whether it is a folder.
type target struct {
	path string
	dir  bool
}

// targets returns where t's copies go.
func (t *task) targets() []target {
	var out []target
	for _, op := range t.plan {
		if op.Kind == mutation.OpCopy {
			out = append(out, target{path: mutation.Target(t.dir, op.To), dir: strings.HasSuffix(op.To, "/")})
		}
	}
	return out
}

// makes reports whether one of t's copies goes to path.
func (t *task) makes(path string) bool {
	for _, to := range t.targets() {
		if to.path == path {
			return true
		}
	}
	return false
}

// ghostOf returns the task whose copy goes to path, or nil.
func (m *Model) ghostOf(path string) *task {
	for _, t := range m.tasks {
		if t.makes(path) {
			return t
		}
	}
	return nil
}

// ghostAside is what a ghost row says on its right: how far its copy got,
// or that it waits for the one before; "" for a row that is no ghost.
func (m *Model) ghostAside(path string) string {
	t := m.ghostOf(path)
	switch {
	case t == nil:
		return ""
	case t.cancel == nil:
		return stDir.Render("waiting")
	case t.stopping:
		return stDir.Render("stopping…")
	}
	// By bytes once they are summed, else by entries; a total not known
	// (0, or -1: a folder not counted) has no bar.
	done, all := t.progress.Bytes.Load(), t.bytes.Load()
	if all <= 0 {
		done, all = t.progress.Entries.Load(), t.total
	}
	if all <= 0 {
		return stDir.Render("copying " + humanSize(t.progress.Bytes.Load()))
	}
	done = min(done, all)
	full := int(done * ghostBar / all)
	return stNameSel.Render(strings.Repeat("█", full)) + stDir.Render(strings.Repeat("░", ghostBar-full)) +
		stDir.Render(fmt.Sprintf("  %3d%%", done*100/all))
}

// ghostRefuses says, for an action on the selected row, why it cannot be
// done, if that row is a ghost: there is no file there yet.
func (m *Model) ghostRefuses() bool {
	if len(m.results) == 0 || m.ghostOf(m.results[m.cursor].Entry.Path) == nil {
		return false
	}
	m.setNotice("still copying: there is nothing there yet", false)
	return true
}
