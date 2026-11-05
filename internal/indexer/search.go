package indexer

import (
	"runtime"
	"strings"
	"sync"
)

// minChunk is the smallest number of entries worth a goroutine of its own.
// Starting a goroutine and joining it costs about a microsecond; scanning
// 8192 entries costs around half a millisecond, so below this size the
// overhead would eat the gain.
const minChunk = 8192

// matchAll returns the positions in snap of every entry whose path contains
// query as a case-insensitive subsequence, in ascending order.
//
// It fans the scan out over all available cores: snap is cut into one
// contiguous chunk per worker, each worker collects matches into its own
// slice, and the slices are concatenated in chunk order (fan-in). Workers
// share nothing they write to, so there is no lock and no channel, and the
// result is identical to a sequential scan.
func matchAll(snap []Entry, query string) []int32 {
	return matchAllN(snap, strings.ToLower(query), runtime.GOMAXPROCS(0))
}

// matchAllN is matchAll with an explicit worker count, so tests and
// benchmarks can compare one core against many.
func matchAllN(snap []Entry, lowerQuery string, workers int) []int32 {
	workers = min(workers, max(1, len(snap)/minChunk))
	if workers == 1 {
		return matchRange(snap, lowerQuery, 0)
	}

	// Each worker writes only parts[w]. Distinct elements of a slice are
	// distinct memory, so concurrent writes to them are not a data race,
	// and wg.Wait makes all of them visible to the code below.
	parts := make([][]int32, workers)
	chunk := (len(snap) + workers - 1) / workers // ceiling division
	var wg sync.WaitGroup
	for w := range workers {
		lo := w * chunk
		hi := min(lo+chunk, len(snap))
		wg.Go(func() {
			parts[w] = matchRange(snap[lo:hi], lowerQuery, lo)
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
