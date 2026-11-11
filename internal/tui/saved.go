package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// The index of the home is saved after every full crawl of it and read at
// the next start, so search works at once instead of after a crawl. Both
// scopes start from it: GLOBAL whole, LOCAL with the part below its folder.
// Then each is crawled in the background, as always, into an index of its
// own that replaces the saved one when complete (see scope.next): what
// changed while fndr was closed shows up within a crawl, and until then
// the user searches what was there last time.
//
// Saving is on unless the cache folder cannot be found: savedPath empty.

// savedMsg carries the saved index, read off the UI goroutine.
type savedMsg struct {
	saved indexer.Saved
	err   error
}

// indexSavedMsg reports a save of the home's index.
type indexSavedMsg struct{ err error }

// change is an edit made to an index while a crawl was refreshing it,
// replayed on the crawl's index before it replaces the old one.
type change struct {
	remove []string
	add    []indexer.Entry
}

// loadSaved reads the saved index of the home.
func loadSaved(path string) tea.Cmd {
	return func() tea.Msg {
		s, err := indexer.Load(path)
		return savedMsg{saved: s, err: err}
	}
}

// savedLoaded starts both scopes, from the saved index if it is one of
// this home crawled under the rules of now, and starts their crawls. A
// missing or unusable index is not reported: it is a cache, and the crawl
// that rebuilds it is what would have run anyway.
//
// The user may have been faster than the disk: Tab, or a folder entered,
// in the milliseconds the read took. A scope whose crawl has started is
// left to it.
func (m *Model) savedLoaded(msg *savedMsg) tea.Cmd {
	s := msg.saved
	usable := msg.err == nil && s.Root == m.home && s.Rules == m.prune.Rules()

	var cmds []tea.Cmd
	if m.global == nil {
		m.global = &scope{root: m.home, ix: &indexer.Index{}}
		if usable {
			m.global.ix.Add(s.Entries)
			m.global.seed = s.Entries
		}
		cmds = append(cmds, m.startCrawl(m.global))
	}
	if m.local.stop == nil {
		if usable && m.local.ix.Len() == 0 && within(m.local.root, m.home) && m.prune.Reaches(m.home, m.local.root) {
			m.local.ix.Add(indexer.Under(s.Entries, m.local.root))
		}
		cmds = append(cmds, m.startCrawl(m.local))
	}
	if m.query != "" {
		cmds = append(cmds, m.search())
	}
	return tea.Batch(cmds...)
}

// saveIndex writes the home's index as it is now. The snapshot never
// changes, so the command may read it on its own goroutine.
func (m *Model) saveIndex() tea.Cmd {
	path, s := m.savedPath, indexer.Saved{Root: m.home, Rules: m.prune.Rules(), Entries: m.global.ix.Snapshot()}
	return func() tea.Msg { return indexSavedMsg{err: indexer.Save(path, s)} }
}

// indexSaved says if the save failed: the next start would crawl, and the
// user should know why it is not instant.
func (m *Model) indexSaved(msg indexSavedMsg) {
	if msg.err != nil {
		m.setNotice("index not saved: "+msg.err.Error(), true)
	}
}
