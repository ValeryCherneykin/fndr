package tui

import (
	"errors"
	"sync"

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
		if m.local.root == m.home && m.local.stop == nil {
			// fndr is in the home: LOCAL's index would be GLOBAL's, every
			// path held twice (on a home of 320k files, 50 MB more). One
			// scope serves both, and LOCAL is caught up, not crawled.
			m.global = m.local
		}
		if usable {
			m.global.ix.Add(s.Entries)
			m.global.seed = s.Entries
		}
		cmds = append(cmds, m.startCrawl(m.global))
	}
	if m.local != m.global && m.local.stop == nil {
		if usable && m.local.ix.Len() == 0 && within(m.local.root, m.home) && m.prune.Reaches(m.home, m.local.root) {
			m.local.ix.Add(indexer.Under(s.Entries, m.local.root))
		}
		cmds = append(cmds, m.startCrawl(m.local))
	}
	if !m.browsing() {
		cmds = append(cmds, m.search())
	}
	return tea.Batch(cmds...)
}

// indexSave is a save of the home's index on its way to the disk.
//
// The program can end while it runs: the user quits within the few dozen
// milliseconds it takes after a crawl. Cut off there, it would leave its
// temporary file behind, the name of every file in the home in it, and
// no index. So once makes it run a single time, whoever calls write
// first: the command it was handed to, or Close at the end, which waits
// for one running and writes one whose command never ran.
type indexSave struct {
	once sync.Once
	path string
	s    indexer.Saved
	err  error
}

func (w *indexSave) write() error {
	w.once.Do(func() { w.err = indexer.Save(w.path, w.s) })
	return w.err
}

// saveIndex writes the home's index as it is now. The snapshot never
// changes, so the command may read it on its own goroutine.
func (m *Model) saveIndex() tea.Cmd {
	w := &indexSave{path: m.savedPath, s: indexer.Saved{Root: m.home, Rules: m.prune.Rules(), Entries: m.global.ix.Snapshot()}}
	m.indexSaves = append(m.indexSaves, w)
	return func() tea.Msg { return indexSavedMsg{err: w.write()} }
}

// saveIndexOrNil is saveIndex, or nil with no file to save to.
func (m *Model) saveIndexOrNil() tea.Cmd {
	if m.savedPath == "" {
		return nil
	}
	return m.saveIndex()
}

// Close finishes what fndr writes in the background, for the end of the
// program: the saves of the index handed out, and the history.
func (m *Model) Close() error {
	var errs []error
	for _, w := range m.indexSaves {
		errs = append(errs, w.write())
	}
	return errors.Join(append(errs, m.SaveHistory())...)
}

// indexSaved says if the save failed: the next start would crawl, and the
// user should know why it is not instant.
func (m *Model) indexSaved(msg indexSavedMsg) {
	if msg.err != nil {
		m.setNotice("index not saved: "+msg.err.Error(), true)
	}
}
