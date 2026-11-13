package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// The crawl fills the index in the background while the user already
// searches it. Batches reach the UI one message at a time, so the index
// only ever changes on the UI goroutine.

// Messages of the crawl, which runs in its own goroutine (see runCrawl).
type (
	crawlStartedMsg struct{ stream *crawlStream }
	batchMsg        struct {
		gen     int
		entries []indexer.Entry
	}
	crawlDoneMsg struct {
		gen   int
		stats indexer.Stats
		err   error
	}
)

// scope is a tree to search: the index of everything below root, and the
// crawl that fills it. LOCAL search is the scope of the folder fndr is in,
// narrowed to cwd.
type scope struct {
	root string
	ix   *indexer.Index

	// Every crawl gets a new gen, from one counter for the whole model, and
	// a message of the crawl names it: a batch of a crawl replaced since,
	// or of another scope's, cannot leak into this index.
	gen      int
	stop     context.CancelFunc
	stream   *crawlStream
	crawling bool
	skipped  int   // paths the crawl could not read
	err      error // why the crawl failed, if it did

	// A crawl of a scope that already has entries (a saved index, see
	// saved.go) refreshes it: it fills next, and ix, still searched, is
	// replaced only once next is complete. Filling ix itself would show
	// every entry twice until the crawl ended, and anything deleted
	// since would never leave. changes are the edits made to ix
	// meanwhile (renames, OIL), replayed on next before the swap: the
	// crawl may have read a folder before it changed.
	next    *indexer.Index
	changes []change

	// seed is the saved index ix started from, sorted, while it is what
	// ix holds: the crawl then only catches up with it (indexer.Refresh),
	// reading the folders that changed. Only the home's: the saved index
	// is a crawl of the home, and a crawl from another root sees other
	// .gitignore files above it, so would not agree with it.
	seed []indexer.Entry
}

// crawlStream connects the crawl goroutine to the UI. Batches are pulled one
// at a time by a command, the standard Bubble Tea way to consume a channel:
// each batchMsg schedules the read of the next one.
type crawlStream struct {
	gen     int
	batches chan []indexer.Entry
	done    chan crawlDoneMsg
}

// startCrawl stops any crawl running for s and starts one filling s.ix,
// which the caller has made fresh.
func (m *Model) startCrawl(s *scope) tea.Cmd {
	if s.stop != nil {
		s.stop()
	}
	m.crawlGen++
	ctx, stop := context.WithCancel(m.ctx)
	s.gen, s.stop, s.stream = m.crawlGen, stop, nil
	s.crawling, s.skipped, s.err = true, 0, nil
	s.next, s.changes = nil, nil
	if s.ix.Len() > 0 {
		s.next = &indexer.Index{}
	}
	return runCrawl(ctx, s.gen, s.root, m.prune, s.seed)
}

// scopes returns the scopes there are, each once. LOCAL and GLOBAL are one
// when fndr is in the home (see savedLoaded): an edit applied to both by
// name would be applied twice, and show twice in search.
func (m *Model) scopes() []*scope {
	if m.global == nil || m.global == m.local {
		return []*scope{m.local}
	}
	return []*scope{m.local, m.global}
}

// crawled returns the scope whose current crawl is gen, or nil if that
// crawl has been replaced.
func (m *Model) crawled(gen int) *scope {
	for _, s := range m.scopes() {
		if s.gen == gen {
			return s
		}
	}
	return nil
}

// runCrawl launches indexer.Crawl in its own goroutine. A small buffer lets
// the crawl run a few batches ahead of the UI instead of waiting on every
// handoff. The goroutine always ends: Crawl returns when ctx is canceled, and
// done has room for its one result even if nobody reads it.
func runCrawl(ctx context.Context, gen int, root string, prune *indexer.Prune, seed []indexer.Entry) tea.Cmd {
	return func() tea.Msg {
		s := &crawlStream{
			gen:     gen,
			batches: make(chan []indexer.Entry, 4),
			done:    make(chan crawlDoneMsg, 1),
		}
		go func() {
			var (
				stats indexer.Stats
				err   error
			)
			if seed != nil {
				stats, err = indexer.Refresh(ctx, root, prune, seed, s.batches)
			} else {
				stats, err = indexer.Crawl(ctx, root, prune, s.batches)
			}
			close(s.batches)
			s.done <- crawlDoneMsg{gen: gen, stats: stats, err: err}
		}()
		return crawlStartedMsg{stream: s}
	}
}

// crawlStarted starts pulling batches from a new crawl, unless another one
// has replaced it before it started; that one's context is canceled.
func (m *Model) crawlStarted(msg *crawlStartedMsg) tea.Cmd {
	s := m.crawled(msg.stream.gen)
	if s == nil {
		return nil
	}
	s.stream = msg.stream
	return s.stream.next()
}

// batch adds a batch of the crawl to the index and asks for the next one.
func (m *Model) batch(msg *batchMsg) tea.Cmd {
	s := m.crawled(msg.gen)
	if s == nil {
		return nil
	}
	if s.next != nil {
		s.next.Add(msg.entries)
		return s.stream.next() // nothing searched changes until the swap
	}
	s.ix.Add(msg.entries)
	cmd := s.stream.next()
	// Refresh results as the index grows, but never queue searches: if one
	// is running, its reply triggers the next (see searched). A batch of
	// the scope not searched now changes nothing on screen.
	if s == m.current() && !m.browsing() && !m.searching {
		return tea.Batch(cmd, m.search())
	}
	return cmd
}

// crawlDone records how the crawl ended, puts a complete refresh in place
// of what it refreshed, and searches the complete index. A full crawl of
// the home is saved for the next start.
func (m *Model) crawlDone(msg *crawlDoneMsg) tea.Cmd {
	s := m.crawled(msg.gen)
	if s == nil {
		return nil
	}
	s.crawling = false
	s.skipped = msg.stats.Skipped
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		s.err = msg.err
	}
	complete := msg.err == nil
	if s.next != nil {
		if complete {
			for _, c := range s.changes {
				// What a change adds goes first out of next, with all below
				// it: the crawl may have found it too, and twice is a bug.
				remove := c.remove
				for i := range c.add {
					remove = append(remove, c.add[i].Path)
				}
				s.next.Update(remove, c.add)
			}
			s.ix, s.seed = s.next, nil // in crawl order now: no longer sorted
		}
		s.next, s.changes = nil, nil // a crawl cut short leaves the old index
	}

	var cmds []tea.Cmd
	if complete && s == m.global {
		// The home's: a crawl of a folder entered is small, and a full
		// collection after each would be work for nothing. After the save,
		// not beside it: the save's own buffers (a sorted copy, the encoded
		// file) are garbage only once it is done, and given back by a
		// release run alongside, they stayed: 70 MB on a home of 320k.
		cmds = append(cmds, m.releaseAfter(m.saveIndexOrNil()))
	}
	if s == m.current() && !m.browsing() {
		cmds = append(cmds, m.search())
	}
	return tea.Batch(cmds...)
}

// next returns a command that waits for the next batch, or for the final
// result once the batches are exhausted.
func (s *crawlStream) next() tea.Cmd {
	return func() tea.Msg {
		if batch, ok := <-s.batches; ok {
			return batchMsg{gen: s.gen, entries: batch}
		}
		return <-s.done
	}
}

// releaseAfter returns cmd (nil: none) followed, in the same command, by
// handing back to the system what the crawl and cmd used and no longer
// hold. A crawl's peak (a saved index read, the index it replaces, a
// Lstat per file) and a save's buffers are gone once they end, but Go
// keeps freed memory for minutes in case it is needed again: on a home of
// 320k files fndr held some 80 MB more than its index for nothing, then
// 70 MB more while the release ran beside the save instead of after it.
// In a command: the collection it forces runs beside the interface.
func (m *Model) releaseAfter(cmd tea.Cmd) tea.Cmd {
	release := m.release
	return func() tea.Msg {
		var msg tea.Msg
		if cmd != nil {
			msg = cmd()
		}
		release()
		return msg
	}
}
