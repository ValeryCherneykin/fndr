package search

import (
	"slices"
	"strings"
	"time"

	"github.com/ValeryCherneykin/fndr/internal/filter"
	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// Narrowing holds the candidates from one search. It is immutable and can be
// passed to another request on the same snapshot.
type Narrowing struct {
	key     searchKey
	matched []int32
}

// searchKey captures eligibility. Limits and frecency can change because
// matched holds all eligible positions, including those outside the top K.
type searchKey struct {
	text  string
	under string
	dirs  bool
	mode  MatchMode
	tags  filter.Stack
	day   calendarDay
	snap  *indexer.Entry
	n     int
}

// Include the location because calendar tags may span a clock change.
type calendarDay struct {
	year, day int
	location  *time.Location
}

func keyFor(snap []indexer.Entry, r Request, text string, dirs bool, now time.Time) searchKey {
	key := searchKey{
		text: strings.ToLower(text), under: r.Under, dirs: dirs, mode: r.Mode, tags: r.Tags,
		day: calendarDay{year: now.Year(), day: now.YearDay(), location: now.Location()},
		n:   len(snap),
	}
	if len(snap) != 0 {
		key.snap = &snap[0]
	}
	return key
}

// narrows reports whether every match of next must also have matched k.
// Appends to the index change the length, and replacements change the backing
// array even when the length is unchanged.
func (k *searchKey) narrows(next *searchKey) bool {
	return k.snap == next.snap && k.n == next.n && k.under == next.under &&
		k.dirs == next.dirs && k.mode == next.mode && k.day == next.day &&
		strings.HasPrefix(next.text, k.text) && slices.EqualFunc(k.tags, next.tags, sameTag)
}

func sameTag(a, b filter.Tag) bool {
	return a.String() == b.String()
}

func (n *Narrowing) within(next searchKey) []int32 {
	if n == nil || !n.key.narrows(&next) {
		return nil
	}
	return n.matched
}
