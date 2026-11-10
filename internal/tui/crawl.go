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
	return runCrawl(ctx, s.gen, s.root, m.prune)
}

// crawled returns the scope whose current crawl is gen, or nil if that
// crawl has been replaced.
func (m *Model) crawled(gen int) *scope {
	for _, s := range []*scope{m.local, m.global} {
		if s != nil && s.gen == gen {
			return s
		}
	}
	return nil
}

// runCrawl launches indexer.Crawl in its own goroutine. A small buffer lets
// the crawl run a few batches ahead of the UI instead of waiting on every
// handoff. The goroutine always ends: Crawl returns when ctx is canceled, and
// done has room for its one result even if nobody reads it.
func runCrawl(ctx context.Context, gen int, root string, prune *indexer.Prune) tea.Cmd {
	return func() tea.Msg {
		s := &crawlStream{
			gen:     gen,
			batches: make(chan []indexer.Entry, 4),
			done:    make(chan crawlDoneMsg, 1),
		}
		go func() {
			stats, err := indexer.Crawl(ctx, root, prune, s.batches)
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
	s.ix.Add(msg.entries)
	cmd := s.stream.next()
	// Refresh results as the index grows, but never queue searches: if one
	// is running, its reply triggers the next (see searched). A batch of
	// the scope not searched now changes nothing on screen.
	if s == m.current() && m.query != "" && !m.searching {
		return tea.Batch(cmd, m.search())
	}
	return cmd
}

// crawlDone records how the crawl ended and searches the complete index.
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
	if s == m.current() && m.query != "" && !m.searching {
		return m.search()
	}
	return nil
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
