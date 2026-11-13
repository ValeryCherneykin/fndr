package tui

import (
	"strings"
	"time"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// Typing narrows: what "file_7" matches, "file_7_" can only match a part
// of, so the next key searches what the last one matched, not the whole
// index again, as fzf does. On a home of 300k files, on two cores, a key
// went from 11 ms of searching to under 1.
//
// Only when nothing but the text changed, and the new text goes on from
// the old: the same snapshot of the index (a crawl that added entries
// makes another), the same folder, folders-only, tags, and day (a tag
// such as @today means another day after midnight). Anything else, or any
// doubt, searches the whole index, as before.

// searchKey is what a search's matches depend on.
type searchKey struct {
	text  string // lower case, as matching is
	under string
	dirs  bool
	tags  string
	day   string
	snap  *indexer.Entry // the snapshot: its first entry and its length
	n     int
}

// narrowing is the last search's matches, for the next to search within.
type narrowing struct {
	key     searchKey
	matched []int32
}

// keyOf returns the key of a search of snap for text, now.
func (m *Model) keyOf(snap []indexer.Entry, text string, dirs bool) searchKey {
	k := searchKey{text: strings.ToLower(text), under: m.under(), dirs: dirs, tags: m.searchLabelTags(), day: time.Now().Format(time.DateOnly), n: len(snap)}
	if len(snap) > 0 {
		k.snap = &snap[0]
	}
	return k
}

// narrows reports whether a search for next may look only at what a
// search for k matched.
func (k searchKey) narrows(next searchKey) bool {
	return k.under == next.under && k.dirs == next.dirs && k.tags == next.tags && k.day == next.day &&
		k.snap == next.snap && k.n == next.n && strings.HasPrefix(next.text, k.text)
}

// within returns the positions to search for next, or nil for all.
func (m *Model) within(next searchKey) []int32 {
	if m.narrow == nil || !m.narrow.key.narrows(next) {
		return nil
	}
	return m.narrow.matched
}

// searchLabelTags is the tags as text, the same tags giving the same text.
func (m *Model) searchLabelTags() string {
	parts := make([]string, len(m.tags))
	for i, t := range m.tags {
		parts[i] = t.String()
	}
	return strings.Join(parts, " ")
}
