package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// Search runs off the UI goroutine on a snapshot of the index, one at a
// time: each new search cancels the last, and a reply is shown only if it
// answers the latest query.

// searchMsg carries the results of one search.
type searchMsg struct {
	seq     int
	results []indexer.Result
	total   int
	indexed int // snapshot length the search ran on
	err     error

	// key and matched are the search and all it matched, for the next key
	// to search within (narrow.go).
	key     searchKey
	matched []int32
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

	text, dirs := strings.CutSuffix(m.query, "/")
	snap := m.current().ix.Snapshot()
	q := indexer.Query{Text: text, Under: m.under(), Dirs: dirs, Boost: m.boosts}
	key := m.keyOf(snap, text, dirs)
	q.Within = m.within(key)   // nil: the whole index
	tags, seq := m.tags, m.seq // never changed in place: safe on the search's goroutines
	return func() tea.Msg {
		defer cancel() // release the context once done; canceling twice is harmless
		if len(tags) > 0 {
			// Here, not in Update: a has: tag goes over the whole snapshot.
			q.Keep = tags.Keep(snap, q.Under, time.Now())
		}
		rs, total, matched, err := indexer.SearchMatches(ctx, snap, q)
		return searchMsg{seq: seq, results: rs, total: total, indexed: len(snap), err: err, key: key, matched: matched}
	}
}

// searcher returns the search for what is typed and tagged now, at most
// limit results (0: indexer.DefaultLimit), to run off the UI goroutine.
// It holds everything it needs: the user may type on while it runs.
//
// whole keeps only entries holding what is typed whole, in one piece: the
// search for OIL, where a fuzzy match is not one to edit (oilsearch.go).
func (m *Model) searcher(limit int, whole bool) func(context.Context) (rs []indexer.Result, total, indexed int, err error) {
	// A trailing "/" asks for folders only, the way a path names a folder:
	// "internal/" finds the folder, not the files that live in it.
	text, dirs := strings.CutSuffix(m.query, "/")
	snap := m.current().ix.Snapshot()
	q := indexer.Query{Text: text, Under: m.under(), Dirs: dirs, Boost: m.boosts, Limit: limit}
	tags := m.tags // never changed in place: safe on the search's goroutines
	return func(ctx context.Context) ([]indexer.Result, int, int, error) {
		if len(tags) > 0 {
			// Here, not in Update: a has: tag goes over the whole snapshot.
			q.Keep = tags.Keep(snap, q.Under, time.Now())
		}
		if whole && text != "" {
			q.Keep = holdsWhole(q.Keep, text, q.Under)
		}
		rs, total, err := indexer.Search(ctx, snap, q)
		return rs, total, len(snap), err
	}
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
	m.narrow = nil
	if msg.matched != nil { // nil: most matched, nothing to gain (indexer.SearchMatches)
		m.narrow = &narrowing{key: msg.key, matched: msg.matched}
	}
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

// holdsWhole adds to keep (nil: keep all) that the path below under holds
// text in one piece, in any case: file_7_99 in file_7_99.go, not spread
// over file_70_99 or file_7_909 as a fuzzy match may be. It runs on what
// the cheap fuzzy test let through, once, for OIL: not worth a faster
// path than a lower-case copy.
func holdsWhole(keep func(indexer.Entry) bool, text, under string) func(indexer.Entry) bool {
	lower := strings.ToLower(text)
	return func(e indexer.Entry) bool {
		rel := strings.TrimPrefix(e.Path, under)
		return strings.Contains(strings.ToLower(rel), lower) && (keep == nil || keep(e))
	}
}
