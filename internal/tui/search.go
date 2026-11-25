package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/search"
)

// Search runs off the UI goroutine on a snapshot of the index, one at a
// time: each new search cancels the last, and a reply is shown only if it
// answers the latest query.

// searchMsg carries the results of one search.
type searchMsg struct {
	seq     int
	results []search.Result
	total   int
	indexed int // snapshot length the search ran on
	err     error

	narrow *search.Narrowing
}

// setQuery replaces the query, resets the selection to the top, and starts a
// search for it.
func (m *Model) setQuery(q string) tea.Cmd {
	if q == m.query {
		return nil
	}
	m.query = q
	m.qpos = min(m.qpos, len(q))
	m.cursor, m.offset, m.picked = 0, 0, ""
	return m.search()
}

// search cancels any running search and starts one for the current query on
// a snapshot of the index. The search itself runs in the returned command,
// off the UI goroutine.
func (m *Model) search() tea.Cmd {
	if m.cancelSearch != nil {
		m.cancelSearch()
	}
	m.seq++
	if m.browsing() {
		m.searching, m.cancelSearch = false, nil
		m.showListing()
		return nil
	}

	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelSearch = cancel
	m.searching = true

	snap := m.current().ix.Snapshot()
	req, seq := m.searchRequest(), m.seq
	req.TrackMatches, req.Previous = true, m.narrow
	return func() tea.Msg {
		defer cancel() // release the context once done; canceling twice is harmless
		resp, err := req.Run(ctx, snap, time.Now())
		return searchMsg{seq: seq, results: resp.Results, total: resp.Total, indexed: len(snap), err: err, narrow: resp.Narrowing}
	}
}

func (m *Model) searchRequest() search.Request {
	return search.Request{Query: m.query, Under: m.under(), Tags: m.tags, Boost: m.boosts}
}

// searched shows the results of the latest search; replies of older ones
// are stale (the query changed while they ran) and dropped.
func (m *Model) searched(msg *searchMsg) tea.Cmd {
	if msg.seq != m.seq {
		return nil
	}
	m.searching = false
	if msg.err != nil {
		return nil // canceled; a newer search is already on its way
	}
	m.narrow = msg.narrow
	m.results, m.total = msg.results, msg.total
	m.cursor = m.pickedRow()
	m.scrollToCursor()
	// The crawl added entries while this search ran: search again, so the
	// list keeps up with indexing without a search per batch.
	if m.current().ix.Len() > msg.indexed {
		return m.search()
	}
	return nil
}
