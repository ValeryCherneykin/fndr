package indexer

import (
	"cmp"
	"context"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// DefaultLimit is the number of results Search returns when Query.Limit is
// not set: more than any terminal shows at once, few enough to rank cheaply.
const DefaultLimit = 100

// chunkSize is how many entries a search worker claims at a time. Small
// enough that a large snapshot splits into many more chunks than there are
// cores, so fast cores keep claiming work while slow ones finish theirs.
// Large enough that claiming (one atomic add) and checking for cancellation
// are negligible next to scanning 4096 paths, roughly a quarter of a
// millisecond. It also bounds how long a canceled search keeps running.
const chunkSize = 4096

// Query describes one search.
type Query struct {
	// Text is what the user typed, matched fuzzily and case-insensitively
	// against the part of the path below Under (the whole path if Under is
	// empty). The directories above Under are the same for every entry;
	// letters that happen to occur there would match everything. An empty
	// Text matches nothing: listing a directory is a different operation.
	Text string

	// Under restricts the search to entries strictly below this directory,
	// which must be absolute and cleaned. Empty means the whole index. This
	// is how one index serves both scopes: GLOBAL leaves it empty, LOCAL sets
	// it to the current directory.
	Under string

	// Limit caps the number of results; zero or less means DefaultLimit.
	Limit int
}

// Result is one search hit.
type Result struct {
	Entry Entry
	Score int

	// Positions holds the byte offsets in Entry.Path of the matched
	// characters, ascending, for highlighting.
	Positions []int
}

// Search returns the best matches for q in snap, best first, and the total
// number of entries that matched. It returns ctx.Err() if ctx is canceled
// before it finishes, so a search made stale by the next keystroke stops
// within one chunk instead of running to the end.
//
// The work happens in three stages, each paying only for what survived the
// previous one:
//
//  1. Filter: every entry, on every core, gets the cheap subsequence test
//     (hasSubsequenceFold). Most entries stop here.
//  2. Rank: survivors are scored (fuzzyScore) and offered to a per-worker
//     top-K heap that keeps only the Limit best. Nothing else is stored, so
//     50,000 matches cost a 100-element heap, not a 50,000-element sort.
//  3. Finish: the per-worker heaps are merged and sorted, and highlight
//     positions are computed for the final results only.
//
// Ties are broken by shorter path, then by path, so the order is stable from
// one keystroke to the next and the list on screen does not jump around.
func Search(ctx context.Context, snap []Entry, q Query) (results []Result, total int, err error) {
	return search(ctx, snap, q, runtime.GOMAXPROCS(0))
}

// search is Search with an explicit worker count, so tests can check that the
// result does not depend on it.
func search(ctx context.Context, snap []Entry, q Query, workers int) (results []Result, total int, err error) {
	if q.Text == "" {
		return nil, 0, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	lq := strings.ToLower(q.Text)
	from := tailStart(q.Under)

	chunks := (len(snap) + chunkSize - 1) / chunkSize // ceiling division
	workers = max(1, min(workers, chunks))

	// Dynamic scheduling: workers claim chunks from a shared atomic counter,
	// so fast cores do more of the work instead of waiting for slow ones.
	var next atomic.Int64

	// Each worker builds its heap and count in local variables and stores
	// them once at the end. Updating tops[w] directly in the hot loop would
	// cause false sharing: neighbouring elements share a CPU cache line, and
	// every write by one core would invalidate that line for the others.
	tops := make([]topK, workers)

	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			top := topK{snap: snap, limit: limit}
			for ctx.Err() == nil {
				c := int(next.Add(1) - 1)
				if c >= chunks {
					break
				}
				lo := c * chunkSize
				hi := min(lo+chunkSize, len(snap))
				for i := lo; i < hi; i++ {
					e := &snap[i]
					if q.Under != "" && !isBelow(e.Path, q.Under) {
						continue
					}
					if !hasSubsequenceFold(e.Path[from:], lq) {
						continue
					}
					score, _ := fuzzyScore(e, from, lq, nil)
					top.offer(candidate{idx: int32(i), score: int32(score)}) //nolint:gosec // see candidate
				}
			}
			tops[w] = top
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}

	// Merge. The global best `limit` are among the union of each worker's
	// best `limit`, so this is exact, not an approximation.
	var all []candidate
	for _, t := range tops {
		total += t.count
		all = append(all, t.items...)
	}
	slices.SortFunc(all, func(a, b candidate) int { return rank(snap, a, b) })
	if len(all) > limit {
		all = all[:limit]
	}

	// Highlight positions for the final results only, carved out of one
	// shared buffer: one allocation instead of one per result.
	nq := utf8.RuneCountInString(lq)
	buf := make([]int, len(all)*nq)
	results = make([]Result, len(all))
	for i, c := range all {
		e := &snap[c.idx]
		pos := buf[i*nq : (i+1)*nq : (i+1)*nq] // capped, so appending to one result cannot overwrite the next
		fuzzyScore(e, from, lq, pos)
		results[i] = Result{Entry: *e, Score: int(c.score), Positions: pos}
	}
	return results, total, nil
}

// candidate is a match waiting to be ranked: 8 bytes, so a heap of them is
// cheap to reorder. int32 fits both fields: a snapshot cannot reach 2^31
// entries (80 GiB of index), and a score is bounded by a few dozen points per
// path byte.
type candidate struct {
	idx   int32 // position in the snapshot
	score int32
}

// rank orders candidates best first: it returns a negative number when a
// ranks above b. Higher score wins; ties go to the shorter path, then to the
// lexically smaller one, which makes the order total and deterministic.
func rank(snap []Entry, a, b candidate) int {
	if a.score != b.score {
		return cmp.Compare(b.score, a.score)
	}
	pa, pb := snap[a.idx].Path, snap[b.idx].Path
	if len(pa) != len(pb) {
		return cmp.Compare(len(pa), len(pb))
	}
	return strings.Compare(pa, pb)
}

// topK keeps the best `limit` candidates offered to it, plus a count of all
// offers.
//
// It is a binary heap with the worst kept candidate at the root (a min-heap by
// rank). That is the one element every new candidate is compared against: if
// the newcomer is not better than the worst kept one, it is dropped in O(1);
// otherwise it replaces the root and sinks to its place in O(log limit).
//
// It is hand-written rather than built on container/heap, whose interface
// takes `any`: every Push would box the candidate into a heap allocation, in
// the hottest loop of the search.
type topK struct {
	snap  []Entry
	limit int
	items []candidate
	count int
}

func (t *topK) offer(c candidate) {
	t.count++
	if len(t.items) < t.limit {
		t.items = append(t.items, c)
		t.up(len(t.items) - 1)
		return
	}
	if rank(t.snap, c, t.items[0]) >= 0 {
		return // not better than the worst we keep
	}
	t.items[0] = c
	t.down(0)
}

// worse reports whether items[i] ranks below items[j].
func (t *topK) worse(i, j int) bool {
	return rank(t.snap, t.items[i], t.items[j]) > 0
}

// up moves items[i] toward the root while it is worse than its parent.
func (t *topK) up(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if !t.worse(i, parent) {
			return
		}
		t.items[i], t.items[parent] = t.items[parent], t.items[i]
		i = parent
	}
}

// down moves items[i] toward the leaves while one of its children is worse.
func (t *topK) down(i int) {
	n := len(t.items)
	for {
		worst := 2*i + 1 // left child
		if worst >= n {
			return
		}
		if right := worst + 1; right < n && t.worse(right, worst) {
			worst = right
		}
		if !t.worse(worst, i) {
			return
		}
		t.items[i], t.items[worst] = t.items[worst], t.items[i]
		i = worst
	}
}

// tailStart returns the byte offset where paths below dir continue past it:
// "/home/v" + "/" is 8 bytes. Matching starts there. dir is absolute and
// cleaned, so only the filesystem root ends in a separator.
func tailStart(dir string) int {
	switch {
	case dir == "":
		return 0
	case strings.HasSuffix(dir, string(filepath.Separator)):
		return len(dir)
	default:
		return len(dir) + 1
	}
}

// isBelow reports whether path lies strictly below dir. Both must be absolute
// and cleaned.
func isBelow(path, dir string) bool {
	if dir == "" || len(path) <= len(dir) || !strings.HasPrefix(path, dir) {
		return false
	}
	// A prefix match alone is not enough: /a/foobar is not below /a/foo. The
	// byte after the prefix has to be a separator, unless dir already ends
	// with one, which in a cleaned path happens only for the root "/".
	return dir[len(dir)-1] == filepath.Separator || path[len(dir)] == filepath.Separator
}
