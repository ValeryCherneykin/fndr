package indexer

import (
	"sync"
)

// Index is the in-memory list of entries: filled by a crawl, changed by file
// operations, read by search.
//
// The design rule is that a Snapshot never changes. Search takes one under
// the lock in constant time and then scans it with no lock held, so typing
// stays fast while a crawl or a file operation is running. To keep that rule,
// the index only ever changes in two ways:
//
//   - Add appends. It writes only past the end of every existing snapshot.
//   - Update builds a new array (copy-on-write) and swaps it in. Old
//     snapshots keep pointing at the old array, which nobody writes again.
//
// The zero value is an empty index ready to use. An Index must not be copied
// after first use, because it contains a mutex.
type Index struct {
	// mu guards the entries slice header (pointer, length, capacity), not the
	// entries themselves: those are immutable once written.
	//
	// A plain Mutex rather than an RWMutex: Snapshot, Len and Add hold it for
	// nanoseconds, and for sections that short RWMutex's extra bookkeeping
	// costs more than the reader parallelism it buys.
	mu      sync.Mutex
	entries []Entry
}

// Add appends a batch to the index. The batch slice itself is not retained,
// so the caller may reuse it.
func (ix *Index) Add(batch []Entry) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.entries = append(ix.entries, batch...)
}

// Update reflects file operations in the index: it removes every entry at or
// below each path in remove, then appends add. Paths in remove must be
// absolute and cleaned, like Entry.Path.
//
// Removing a directory removes its whole subtree, so the operations map
// directly:
//
//	create:  Update(nil, created)
//	delete:  Update([]string{path}, nil)
//	rename:  Update([]string{oldPath}, entriesUnderNewPath)
//
// With nothing to remove, Update is a plain append and costs only the new
// entries. With removals it copies the surviving entries into a new array:
// about 7 ms and one 40 MB allocation per million entries on an M-series
// Mac (see BenchmarkIndexUpdate), held under the lock. That is acceptable
// because removals come from user actions, not from typing. If profiling
// ever shows that pause, the copy can move outside the lock.
func (ix *Index) Update(remove []string, add []Entry) {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	if len(remove) == 0 {
		ix.entries = append(ix.entries, add...)
		return
	}

	// A fresh array, never the old one: filtering in place would shift
	// entries under the feet of every snapshot that points at it.
	kept := make([]Entry, 0, len(ix.entries)+len(add))
	for i := range ix.entries {
		if !underAny(ix.entries[i].Path, remove) {
			kept = append(kept, ix.entries[i])
		}
	}
	kept = append(kept, add...)
	ix.entries = kept
}

// Snapshot returns the current entries. The result is read-only and never
// changes, whatever Add and Update do afterwards.
func (ix *Index) Snapshot() []Entry {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	// The three-index slice caps the capacity at the length. Without it, an
	// append to the snapshot by the caller would write into the spare
	// capacity of the shared array, exactly where the next Add writes: a
	// data race and corrupted entries. With it, such an append is forced to
	// copy to a fresh array.
	n := len(ix.entries)
	return ix.entries[:n:n]
}

// Len returns the current number of entries.
func (ix *Index) Len() int {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return len(ix.entries)
}

// underAny reports whether path is one of roots or lies below one of them.
//
// It is linear in len(roots), which is fine for user actions that touch a
// handful of paths. A burst of thousands of removals (a watcher after
// `git checkout`) would want sorted roots and a binary search instead.
func underAny(path string, roots []string) bool {
	for _, r := range roots {
		if path == r || isBelow(path, r) {
			return true
		}
	}
	return false
}
