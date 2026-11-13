package indexer

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// TestWithinMatchesFull: a search narrowed to what the query before it
// matched, as typing on narrows, finds exactly what a search of the whole
// snapshot finds: the same results in the same order, scores, positions
// and total. On random chains of queries each extending the one before,
// with a folder, folders only, a Keep, and any number of workers.
func TestWithinMatchesFull(t *testing.T) {
	snap := benchEntries(20_000)
	for i := range snap {
		if i%17 == 0 {
			snap[i].IsDir = true
		}
	}
	keep := func(e Entry) bool { return strings.Contains(e.Path, "a") }
	letters := "abcdefgilmnoprstu_./"
	r := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // seeded on purpose: a failure must replay
	ctx := context.Background()
	for chain := range 200 {
		q := Query{Limit: 1 + r.IntN(60)}
		if r.IntN(3) == 0 {
			q.Under = snap[r.IntN(len(snap))].Path[:strings.LastIndexByte(snap[r.IntN(len(snap))].Path, '/')]
		}
		q.Dirs = r.IntN(5) == 0
		if r.IntN(3) == 0 {
			q.Keep = keep
		}
		var within []int32
		text := ""
		for range 1 + r.IntN(6) {
			text += string(letters[r.IntN(len(letters))])
			full, total, _, err := search(ctx, snap, Query{Text: text, Under: q.Under, Dirs: q.Dirs, Keep: q.Keep, Limit: q.Limit}, 1+r.IntN(8), false)
			if err != nil {
				t.Fatal(err)
			}
			nq := q
			nq.Text, nq.Within = text, within
			got, gotTotal, matched, err := search(ctx, snap, nq, 1+r.IntN(8), true)
			if err != nil {
				t.Fatal(err)
			}
			if gotTotal != total || matched != nil && (len(matched) != total || !slices.IsSorted(matched)) {
				t.Fatalf("chain %d %q: total %d, %d matched (sorted %v), want %d", chain, text, gotTotal, len(matched), slices.IsSorted(matched), total)
			}
			if fmt.Sprint(got) != fmt.Sprint(full) {
				t.Fatalf("chain %d %q within %d: narrowed and full disagree\n got %v\nwant %v", chain, text, len(within), got, full)
			}
			within = matched
		}
	}
}

// TestSearchMatchesAll: what SearchMatches reports matched is every entry
// a search matches, by its position, each once.
func TestSearchMatchesAll(t *testing.T) {
	snap := benchEntries(9_000)
	_, _, matched, err := SearchMatches(context.Background(), snap, Query{Text: "file_12"})
	if err != nil {
		t.Fatal(err)
	}
	var want []int32
	for i := range snap {
		if hasSubsequenceFold(snap[i].Path, "file_12") {
			want = append(want, int32(i)) //nolint:gosec // a test snapshot
		}
	}
	if !slices.Equal(matched, want) {
		t.Errorf("%d matched, want %d", len(matched), len(want))
	}
}

// BenchmarkSearchNarrowed: the next key of a name being typed, file_123
// then file_1234, over 300k: narrowed to what the first matched, against
// the whole snapshot again.
func BenchmarkSearchNarrowed(b *testing.B) {
	snap := benchEntries(300_000)
	ctx := context.Background()
	_, _, within, err := SearchMatches(ctx, snap, Query{Text: "file_123"})
	if err != nil {
		b.Fatal(err)
	}
	b.Run("whole", func(b *testing.B) {
		for b.Loop() {
			_, _, _ = Search(ctx, snap, Query{Text: "file_1234"})
		}
	})
	b.Run("narrowed", func(b *testing.B) {
		for b.Loop() {
			_, _, _, _ = SearchMatches(ctx, snap, Query{Text: "file_1234", Within: within})
		}
	})
	b.ReportMetric(float64(len(within)), "within")
}

// TestSearchMatchesKeepsLittle: with most of the snapshot matching, the
// positions are not kept: narrowing to them would save nothing.
func TestSearchMatchesKeepsLittle(t *testing.T) {
	snap := benchEntries(9_000)
	_, total, matched, err := SearchMatches(context.Background(), snap, Query{Text: "project"})
	if err != nil || total != len(snap) || matched != nil {
		t.Errorf("total %d, %d kept, %v; want all counted and none kept", total, len(matched), err)
	}
}

// TestNarrowedKeepsNarrowing: a search within the matches of the one
// before keeps its own, though nearly all of them match: what counts is
// how little of the whole snapshot that is. Lost, every third key would
// search the whole index again.
func TestNarrowedKeepsNarrowing(t *testing.T) {
	snap := benchEntries(20_000)
	ctx := context.Background()
	_, _, first, err := SearchMatches(ctx, snap, Query{Text: "file_12"})
	if err != nil || first == nil {
		t.Fatalf("file_12: kept %d, %v", len(first), err)
	}
	// file_12g: every file_12 so far is a .go, so all of first match it.
	_, _, next, err := SearchMatches(ctx, snap, Query{Text: "file_12g", Within: first})
	if err != nil || next == nil || len(next) != len(first) {
		t.Errorf("file_12g within file_12: kept %d of %d, %v; want them all", len(next), len(first), err)
	}
}
