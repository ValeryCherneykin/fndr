package indexer

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// chunkSize is how many entries a worker claims at a time. Small enough that
// a large snapshot splits into many more chunks than there are cores, so fast
// cores keep claiming work while slow ones finish theirs. Large enough that
// claiming (one atomic add) and per-chunk bookkeeping are negligible next to
// scanning 4096 paths, roughly a quarter of a millisecond.
const chunkSize = 4096

// matchAll returns the positions in snap of every entry whose path contains
// query as a case-insensitive subsequence, in ascending order.
//
// It spreads the scan over all available cores with dynamic scheduling.
// Splitting snap into one equal part per core would leave the fast cores idle
// while the slowest one finishes, and Apple chips mix performance and
// efficiency cores, so the slowest one is much slower. Instead snap is cut
// into many small chunks, and every worker repeatedly claims the next
// unclaimed chunk from a shared atomic counter until none are left. Fast
// cores simply end up scanning more chunks.
//
// Each chunk's matches go into parts[chunk], indexed by chunk rather than by
// worker, so concatenating parts in order yields exactly the result of a
// sequential scan.
func matchAll(snap []Entry, query string) []int32 {
	return matchAllN(snap, strings.ToLower(query), runtime.GOMAXPROCS(0))
}

// matchAllN is matchAll with an explicit worker count, so tests and
// benchmarks can compare one core against many.
func matchAllN(snap []Entry, lowerQuery string, workers int) []int32 {
	chunks := (len(snap) + chunkSize - 1) / chunkSize // ceiling division
	workers = min(workers, chunks)
	if workers <= 1 {
		return matchRange(snap, lowerQuery, 0)
	}

	// Each chunk is claimed by exactly one worker, so each parts[c] has a
	// single writer. Distinct slice elements are distinct memory, so the
	// writes do not race, and wg.Wait makes them all visible below.
	parts := make([][]int32, chunks)

	// next is the only shared mutable state. An atomic add is a single CPU
	// instruction; a mutex here could put a goroutine to sleep on every claim.
	var next atomic.Int64

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for {
				c := int(next.Add(1) - 1)
				if c >= chunks {
					return
				}
				lo := c * chunkSize
				hi := min(lo+chunkSize, len(snap))
				parts[c] = matchRange(snap[lo:hi], lowerQuery, lo)
			}
		})
	}
	wg.Wait()

	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]int32, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// matchRange scans entries sequentially. base is the position of entries[0]
// in the full snapshot, so the returned positions are global.
//
// Positions are int32 rather than int to halve the memory of large result
// sets. A snapshot cannot exceed 2^31 entries in practice: at 40 bytes each
// that would be 80 GiB of index.
func matchRange(entries []Entry, lowerQuery string, base int) []int32 {
	var out []int32
	for i := range entries {
		if hasSubsequenceFold(entries[i].Path, lowerQuery) {
			out = append(out, int32(base+i)) //nolint:gosec // bounded, see above
		}
	}
	return out
}
