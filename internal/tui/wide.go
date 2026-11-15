package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// A search in what is ignored (ctrl+d): the scope searched, crawled again
// into the folders left out as a project's clutter, build/ and dist/ of a
// .gitignore, node_modules (indexer.Prune.WithIgnored), for the artifact
// or the package's source looked for there. Off by default, and never in
// the saved index: on a home it can be hundreds of thousands of files
// more. A crawl of its own, not a filter of the usual index, which has
// nothing of what is inside those folders.
//
// It lasts until the scope changes: a folder entered, or Tab. What it
// searched is gone with it.

// toggleWide turns the search in what is ignored on, crawling for it, or
// off.
func (m *Model) toggleWide() tea.Cmd {
	if m.wide != nil {
		m.dropWide()
		return m.search()
	}
	base := m.current()
	m.wide = &scope{root: base.root, ix: new(indexer.Index), prune: m.prune.WithIgnored()}
	m.cursor, m.offset, m.picked = 0, 0, ""
	return tea.Batch(m.startCrawl(m.wide), m.search())
}

// dropWide turns the search in what is ignored off, stopping its crawl.
func (m *Model) dropWide() {
	if m.wide == nil {
		return
	}
	if m.wide.stop != nil {
		m.wide.stop()
	}
	m.wide = nil
}
