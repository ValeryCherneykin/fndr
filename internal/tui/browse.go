package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// Browsing works like Finder: an empty query shows the contents of cwd,
// enter goes into a folder, backspace on an empty query goes back out.
// Search follows: typing searches below cwd, so the folder the user is in
// is always the scope.
//
// The listing is read from disk, not from the index (see indexer.List), in
// a command like everything else that touches the disk.

// loadListing reads dir in the background.
func loadListing(dir string) tea.Cmd {
	return func() tea.Msg {
		es, err := indexer.List(dir)
		return listMsg{dir: dir, entries: es, err: err}
	}
}

// listed stores a directory listing, unless the user has moved on since it
// was requested.
func (m *Model) listed(msg listMsg) {
	if msg.dir != m.cwd {
		return
	}
	m.listErr = msg.err
	m.listing = make([]indexer.Result, len(msg.entries))
	for i, e := range msg.entries {
		m.listing[i].Entry = e
	}
	if m.query == "" {
		m.showListing()
	}
}

// showListing puts the listing on screen, selecting the folder just come
// up from, if any: going back out lands where you went in, as in Finder.
func (m *Model) showListing() {
	m.results, m.total = m.listing, len(m.listing)
	m.cursor, m.offset = 0, 0
	if m.listing == nil {
		return // not read yet; keep reveal for when it is
	}
	for i := range m.listing {
		if m.listing[i].Entry.Path == m.reveal {
			m.cursor = i
			break
		}
	}
	m.reveal = ""
	m.scrollToCursor()
}

// activate is enter on the selected row: go into a folder, open a file.
func (m *Model) activate() tea.Cmd {
	if len(m.results) == 0 {
		return nil
	}
	if e := &m.results[m.cursor].Entry; e.IsDir {
		return m.chdir(e.Path, "")
	}
	return m.openSelected()
}

// up goes to the parent of cwd and selects the folder it came from.
func (m *Model) up() tea.Cmd {
	parent := filepath.Dir(m.cwd)
	if parent == m.cwd {
		m.setNotice("already at the top", false)
		return nil
	}
	return m.chdir(parent, m.cwd)
}

// chdir makes dir the current folder: the query is cleared, the listing
// reloaded, and search narrowed to dir. Going above the indexed root
// starts a new crawl from dir, because nothing above root is indexed.
func (m *Model) chdir(dir, reveal string) tea.Cmd {
	m.cwd, m.reveal = dir, reveal
	m.listing, m.listErr = nil, nil
	m.query = ""
	m.search() // cancels any search in flight and shows the (empty) listing

	cmds := []tea.Cmd{loadListing(dir)}
	if !within(dir, m.root) {
		m.root, m.ix = dir, &indexer.Index{}
		cmds = append(cmds, m.startCrawl())
	}
	return tea.Batch(cmds...)
}

// within reports whether path is dir or lies below it. Both are absolute
// and cleaned.
func within(path, dir string) bool {
	if path == dir || dir == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
