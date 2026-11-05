package indexer

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// benchEntries wraps benchPaths in entries.
func benchEntries(n int) []Entry {
	ps := benchPaths(n)
	es := make([]Entry, n)
	for i, p := range ps {
		es[i] = NewEntry(p, 0, 0, false)
	}
	return es
}

// TestMatchAllParallelEqualsSequential is the key property of the fan-out:
// splitting the work must never change the answer, for any size and any
// number of workers, including more workers than chunks.
func TestMatchAllParallelEqualsSequential(t *testing.T) {
	for _, n := range []int{0, 1, chunkSize - 1, chunkSize, 3*chunkSize + 17, 100_000} {
		snap := benchEntries(n)
		for _, query := range []string{"", "intfile9", "nomatch!", "FILE_1"} {
			lq := strings.ToLower(query)
			want := matchRange(snap, lq, 0)
			for _, workers := range []int{1, 2, 3, 8, 64} {
				t.Run(fmt.Sprintf("n=%d/q=%q/w=%d", n, query, workers), func(t *testing.T) {
					got := matchAllN(snap, lq, workers)
					if !slices.Equal(got, want) {
						t.Errorf("got %d matches, want %d (or same count, different order)", len(got), len(want))
					}
				})
			}
		}
	}
}

func TestMatchAll(t *testing.T) {
	snap := entries("/src/main.go", "/docs/readme.md", "/src/Makefile", "/src/mango.go")
	got := matchAll(snap, "MNGO")
	if want := []int32{0, 3}; !slices.Equal(got, want) {
		t.Errorf("matchAll(MNGO) = %v, want %v", got, want)
	}
}

// BenchmarkMatchAll compares one core with all cores over the same snapshot:
// the cost of the first search stage per keystroke.
func BenchmarkMatchAll(b *testing.B) {
	const query = "intfile9"
	for _, n := range []int{100_000, 1_000_000} {
		snap := benchEntries(n)
		lq := strings.ToLower(query)
		b.Run(fmt.Sprintf("n=%d/cores=1", n), func(b *testing.B) {
			for b.Loop() {
				matchAllN(snap, lq, 1)
			}
		})
		b.Run(fmt.Sprintf("n=%d/cores=all", n), func(b *testing.B) {
			for b.Loop() {
				matchAll(snap, query)
			}
		})
	}
}
