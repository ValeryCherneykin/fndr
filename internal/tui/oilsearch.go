package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/search"
)

// OIL over search results: ctrl+o with a query or tags edits what was
// found, not a folder. Every entry found is a line, its path from the
// folder searched ("fndr/debug.log"), so editing a line renames it and
// editing its folders moves it, as ctrl+r does, and the same plan, :w
// then y, carries it out. A buffer of one folder is the special case of
// a search that found exactly what is in it.
//
// After y the buffer is what was written, read again from the disk, not
// the search run again: .jpeg renamed to .jpg under the tag .jpeg would
// otherwise vanish the moment it is done (AGENTS.md, 4.3).

// oilMaxResults is the most a search may find to be edited. ggdG over all
// files of a home would put the home in the trash, and a plan of 80 000
// lines is one nobody reads before y: past this, the search is narrowed
// first.
const oilMaxResults = 10_000

// oilSearch marks a buffer of search results, and says what found them.
type oilSearch struct {
	label     string // the tags and the query, for the header
	fuzzyLeft int    // found by the fuzzy search, left out of the buffer
}

// oilFromSearch loads the results of the search on screen for OIL, all of
// them, off the UI goroutine.
func (m *Model) oilFromSearch() tea.Cmd {
	if sc := m.current(); sc.crawling && sc.next == nil {
		// The first crawl is still finding things: a buffer now would be
		// a part, and ggdG over a part is not what was asked for.
		m.setNotice("still indexing: ctrl+o once the search has everything", true)
		return nil
	}
	// Only what holds the query whole: a fuzzy search for file_7_99 also
	// finds file_70_99 and file_7_909, 1596 of them for 11 that the eye
	// sees on top, and ggdG would trash them all. Fuzzy finds; an edit is
	// of what was meant. The rest is counted, and the header says so.
	snap := m.current().ix.Snapshot()
	whole := m.searchRequest()
	whole.Mode, whole.Limit = search.Contiguous, oilMaxResults+1
	fuzzy := whole
	fuzzy.Mode, fuzzy.Limit = search.Fuzzy, 1
	dir, label, typed, ctx := whole.Under, m.searchLabel(), whole.Query, m.ctx
	return func() tea.Msg {
		state := &oilSearch{label: label}
		resp, err := whole.Run(ctx, snap, time.Now())
		if err == nil && whole.HasText() {
			var count search.Response
			count, err = fuzzy.Run(ctx, snap, time.Now())
			if err == nil {
				state.fuzzyLeft = count.Total - resp.Total
			}
		}
		total := resp.Total
		switch {
		case err != nil:
			return oilMsg{search: state, err: err}
		case total > oilMaxResults:
			return oilMsg{search: state, err: fmt.Errorf("%d found, %d at most: narrow the search first", total, oilMaxResults)}
		case total == 0 && state.fuzzyLeft > 0:
			return oilMsg{search: state, err: fmt.Errorf("nothing holds %q whole; OIL takes no fuzzy match (%d left out)", typed, state.fuzzyLeft)}
		}
		es := make([]indexer.Entry, len(resp.Results))
		for i := range resp.Results {
			es[i] = resp.Results[i].Entry
		}
		return oilMsg{dir: dir, entries: es, search: state}
	}
}

// searchLabel is the tags and the query, as typed.
func (m *Model) searchLabel() string {
	return strings.TrimSpace(m.searchLabelTags() + " " + m.query)
}

// oilReread reads again, for a buffer of results, the entries its lines
// name, off the UI goroutine: after y, or ctrl+r. A line is its path as
// written if that exists, or else the path of the entry it came from, if
// that does: a plan stopped halfway leaves some lines unwritten, and the
// buffer shows the disk, not the wish. An entry with no line left (dd) is
// shown only if it is still there, its trash having failed.
func (st *oilState) oilReread() tea.Cmd {
	var groups [][]string // per line: the paths to try, in order
	// Away items (oilyank.go) are not results: put by p, a line of one is
	// read again by what was written; left out, there is nothing to show.
	lined := make([]bool, st.own)
	for _, l := range st.buf.Lines() {
		var try []string
		if text := strings.TrimSpace(l.Text); text != "" {
			try = append(try, filepath.Clean(strings.TrimSuffix(text, "/")))
		}
		if l.ID > 0 && l.ID <= st.own {
			lined[l.ID-1] = true
			try = append(try, strings.TrimSuffix(st.items[l.ID-1].Name, "/"))
		}
		groups = append(groups, try)
	}
	for i, it := range st.items[:st.own] {
		if !lined[i] {
			groups = append(groups, []string{strings.TrimSuffix(it.Name, "/")})
		}
	}
	dir, state, row := st.dir, st.search, st.buf.Row()
	return func() tea.Msg {
		var es []indexer.Entry
		seen := map[string]bool{}
		for _, try := range groups {
			for _, rel := range try {
				p := filepath.Join(dir, rel)
				info, err := os.Lstat(p)
				if err != nil || seen[p] {
					continue
				}
				seen[p] = true
				es = append(es, indexer.NewEntry(p, info.Size(), info.ModTime().UnixNano(), info.IsDir()))
				break
			}
		}
		return oilMsg{dir: dir, entries: es, search: state, row: row}
	}
}

// oilLine is the line of e in a buffer of dir: its name, or, for results,
// its path from dir; a folder's with a "/" after it, as Oil.nvim shows.
func oilLine(e *indexer.Entry, dir string, fromSearch bool) string {
	name := e.Name()
	if fromSearch {
		name = strings.TrimPrefix(strings.TrimPrefix(e.Path, dir), string(filepath.Separator))
	}
	if e.IsDir {
		name += "/"
	}
	return name
}
