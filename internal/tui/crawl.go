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

// crawlStream connects the crawl goroutine to the UI. Batches are pulled one
// at a time by a command, the standard Bubble Tea way to consume a channel:
// each batchMsg schedules the read of the next one.
type crawlStream struct {
	gen     int
	batches chan []indexer.Entry
	done    chan crawlDoneMsg
}

// startCrawl stops any running crawl and starts one over a fresh index
// rooted at m.root.
func (m *Model) startCrawl() tea.Cmd {
	if m.crawlStop != nil {
		m.crawlStop()
	}
	m.crawlGen++
	ctx, stop := context.WithCancel(m.ctx)
	m.crawlStop = stop
	m.crawling, m.skipped, m.crawlErr = true, 0, nil
	return runCrawl(ctx, m.crawlGen, m.root, m.prune)
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
	if msg.stream.gen != m.crawlGen {
		return nil
	}
	m.crawl = msg.stream
	return m.crawl.next()
}

// batch adds a batch of the crawl to the index and asks for the next one.
func (m *Model) batch(msg *batchMsg) tea.Cmd {
	if msg.gen != m.crawlGen {
		return nil
	}
	m.ix.Add(msg.entries)
	cmd := m.crawl.next()
	// Refresh results as the index grows, but never queue searches: if one
	// is running, its reply triggers the next (see searched).
	if m.query != "" && !m.searching {
		return tea.Batch(cmd, m.search())
	}
	return cmd
}

// crawlDone records how the crawl ended and searches the complete index.
func (m *Model) crawlDone(msg *crawlDoneMsg) tea.Cmd {
	if msg.gen != m.crawlGen {
		return nil
	}
	m.crawling = false
	m.skipped = msg.stats.Skipped
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		m.crawlErr = msg.err
	}
	if m.query != "" && !m.searching {
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
