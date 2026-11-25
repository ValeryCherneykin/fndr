package search

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// benchEntries wraps benchPaths in entries.
func benchEntries(n int) []indexer.Entry {
	ps := benchPaths(n)
	es := make([]indexer.Entry, n)
	for i, p := range ps {
		// Times repeat, so ranking by time has ties to break too.
		es[i] = indexer.NewEntry(p, 0, int64(i*7919%1000), false)
	}
	return es
}

// naiveSearch is the obvious algorithm: score every entry, sort everything,
// keep the first limit. It is the reference that scanResults must agree with,
// whatever the worker count, and the baseline for the benchmark.
func naiveSearch(snap []indexer.Entry, q scanQuery) (paths []string, total int) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if q.Text == "" && q.Keep == nil {
		return nil, 0
	}
	lq := strings.ToLower(q.Text)
	var all []candidate
	for i := range snap {
		if q.Keep != nil && !q.Keep(snap[i]) {
			continue
		}
		if q.Under != "" && !indexer.IsBelow(snap[i].Path, q.Under) {
			continue
		}
		if q.Dirs && !snap[i].IsDir {
			continue
		}
		if s, ok := fuzzyScore(&snap[i], tailStart(q.Under), lq, nil); ok {
			all = append(all, candidate{idx: int32(i), score: int32(s)}) //nolint:gosec // test data is small
		}
	}
	slices.SortFunc(all, func(a, b candidate) int { return rank(snap, q.Text == "", a, b) })
	total = len(all)
	for _, c := range all[:min(limit, len(all))] {
		paths = append(paths, snap[c.idx].Path)
	}
	return paths, total
}

// TestSearchIgnoresRootPrefix: the directories above Under are the same for
// every entry, so letters in them must not match. Without this, "home"
// matches every file below /home/v.
func TestSearchIgnoresRootPrefix(t *testing.T) {
	snap := []indexer.Entry{
		indexer.NewEntry("/home/v/dev/src/main.go", 0, 0, false),
		indexer.NewEntry("/home/v/dev/hd.txt", 0, 0, false),
	}
	tests := []struct {
		text, under string
		want        []string
	}{
		{"home", "/home/v/dev", nil},
		{"hd", "/home/v/dev", []string{"/home/v/dev/hd.txt"}},
		{"srcmain", "/home/v/dev", []string{"/home/v/dev/src/main.go"}},
		{"home", "", []string{"/home/v/dev/hd.txt", "/home/v/dev/src/main.go"}}, // GLOBAL: whole path
		{"homevdev", "/", []string{"/home/v/dev/hd.txt", "/home/v/dev/src/main.go"}},
	}
	for _, tt := range tests {
		rs, _, err := scanResults(context.Background(), snap, scanQuery{Text: tt.text, Under: tt.under})
		if err != nil {
			t.Fatal(err)
		}
		got := resultPaths(rs)
		slices.Sort(got)
		if !slices.Equal(got, tt.want) {
			t.Errorf("scanResults(%q under %q) = %q, want %q", tt.text, tt.under, got, tt.want)
		}
	}
}

func TestSearchDirsOnly(t *testing.T) {
	snap := []indexer.Entry{
		indexer.NewEntry("/r/internal", 0, 0, true),
		indexer.NewEntry("/r/internal/tui", 0, 0, true),
		indexer.NewEntry("/r/internal/tui/model.go", 0, 0, false),
		indexer.NewEntry("/r/internal.md", 0, 0, false),
	}
	rs, total, err := scanResults(context.Background(), snap, scanQuery{Text: "internal", Dirs: true})
	if err != nil {
		t.Fatal(err)
	}
	got := resultPaths(rs)
	slices.Sort(got)
	if want := []string{"/r/internal", "/r/internal/tui"}; !slices.Equal(got, want) || total != 2 {
		t.Errorf("Dirs search = %q (total %d), want only the folders %q", got, total, want)
	}
	// Without Dirs, folders and files are both found.
	if _, total, _ := scanResults(context.Background(), snap, scanQuery{Text: "internal"}); total != 4 {
		t.Errorf("plain search found %d entries, want all 4, folders included", total)
	}
}

// TestSearchBoost: a habit lifts a path above an equally good match, and
// the boost decides the order even when the top-K cut is tight, which is
// why it is applied in the workers, before the cut, not afterwards.
func TestSearchBoost(t *testing.T) {
	snap := []indexer.Entry{
		indexer.NewEntry("/r/a/main.go", 0, 0, false),
		indexer.NewEntry("/r/b/main.go", 0, 0, false),
		indexer.NewEntry("/r/c/main.go", 0, 0, false),
	}
	q := scanQuery{Text: "main", Limit: 1}
	rs, _, _ := scanResults(context.Background(), snap, q)
	if len(rs) != 1 || rs[0].Entry.Path == "/r/c/main.go" {
		t.Fatalf("without a boost got %q; the test needs c not to win on its own", resultPaths(rs))
	}

	q.Boost = map[string]int{"/r/c/main.go": 30}
	rs, _, _ = scanResults(context.Background(), snap, q)
	if len(rs) != 1 || rs[0].Entry.Path != "/r/c/main.go" {
		t.Errorf("with c boosted got %q, want c first", resultPaths(rs))
	}
}

func resultPaths(rs []Result) []string {
	ps := make([]string, len(rs))
	for i := range rs {
		ps[i] = rs[i].Entry.Path
	}
	return ps
}

// TestSearchMatchesNaive is the key property: the parallel top-K search gives
// exactly the answer of scoring and sorting everything, for any snapshot
// size, query, limit and number of workers.
func TestSearchMatchesNaive(t *testing.T) {
	for _, n := range []int{0, 1, chunkSize - 1, chunkSize, 3*chunkSize + 17, 50_000} {
		snap := benchEntries(n)
		for _, text := range []string{"intfile9", "go", "nomatch!", "FILE_1", "", "+go"} {
			for _, limit := range []int{1, 7, 100} {
				q := scanQuery{Text: text, Limit: limit}
				if text == "" || text[0] == '+' { // with a tag: names ending in 1
					q.Text = strings.TrimPrefix(text, "+")
					q.Keep = func(e indexer.Entry) bool { return strings.HasSuffix(e.Name(), "1") }
				}
				wantPaths, wantTotal := naiveSearch(snap, q)
				for _, workers := range []int{1, 3, 8, 64} {
					t.Run(fmt.Sprintf("n=%d/q=%s/limit=%d/w=%d", n, text, limit, workers), func(t *testing.T) {
						rs, total, _, err := scan(context.Background(), snap, q, workers, false)
						if err != nil {
							t.Fatal(err)
						}
						if total != wantTotal {
							t.Errorf("total = %d, want %d", total, wantTotal)
						}
						if got := resultPaths(rs); !slices.Equal(got, wantPaths) {
							t.Errorf("results differ from naive search:\n got  %q\n want %q", got, wantPaths)
						}
					})
				}
			}
		}
	}
}

// TestSearchTagsOnly: tags with nothing typed find every entry they keep,
// the places the user goes to first, then the newest: "all .go files"
// opens on the ones in use, not on the ones with the shortest paths.
func TestSearchTagsOnly(t *testing.T) {
	snap := []indexer.Entry{
		indexer.NewEntry("/a/old.go", 0, 100, false),
		indexer.NewEntry("/a/new.go", 0, 300, false),
		indexer.NewEntry("/a/b/habit.go", 0, 50, false),
		indexer.NewEntry("/a/mid.go", 0, 200, false),
		indexer.NewEntry("/a/notes.md", 0, 999, false),
	}
	goFiles := func(e indexer.Entry) bool { return strings.HasSuffix(e.Name(), ".go") }
	rs, total, err := scanResults(context.Background(), snap, scanQuery{
		Keep: goFiles, Boost: map[string]int{"/a/b/habit.go": 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/a/b/habit.go", "/a/new.go", "/a/mid.go", "/a/old.go"}
	if got := resultPaths(rs); total != 4 || !slices.Equal(got, want) {
		t.Errorf("got %q (%d), want %q", got, total, want)
	}
	if rs, _, _ := scanResults(context.Background(), snap, scanQuery{}); rs != nil {
		t.Errorf("no text, no tags: %d results, want none (the folder is listed instead)", len(rs))
	}
}

func TestSearchRanking(t *testing.T) {
	snap := entries(
		"/main/p.go",
		"/src/domain.go",
		"/p/main.go",
		"/docs/readme.md",
	)
	rs, total, err := scanResults(context.Background(), snap, scanQuery{Text: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3 (readme.md does not match)", total)
	}
	if len(rs) == 0 || rs[0].Entry.Path != "/p/main.go" {
		t.Errorf("results = %q, want /p/main.go first", resultPaths(rs))
	}
}

func TestSearchUnder(t *testing.T) {
	snap := entries("/a/foo/x.go", "/a/foo", "/a/foobar/x.go", "/b/x.go")
	rs, _, err := scanResults(context.Background(), snap, scanQuery{Text: "x", Under: "/a/foo"})
	if err != nil {
		t.Fatal(err)
	}
	// /a/foo itself is not below /a/foo, and /a/foobar is a sibling.
	if got, want := resultPaths(rs), []string{"/a/foo/x.go"}; !slices.Equal(got, want) {
		t.Errorf("Under /a/foo: got %q, want %q", got, want)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	rs, total, err := scanResults(context.Background(), entries("/a", "/b"), scanQuery{})
	if err != nil || rs != nil || total != 0 {
		t.Errorf("empty query: got %v, %d, %v; want nothing", rs, total, err)
	}
}

func TestSearchPositions(t *testing.T) {
	rs, _, err := scanResults(context.Background(), entries("/src/main.go"), scanQuery{Text: "MNGO"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 {
		t.Fatalf("got %d results, want 1", len(rs))
	}
	if got, want := rs[0].Positions, []int{5, 8, 10, 11}; !slices.Equal(got, want) {
		t.Errorf("positions = %v, want %v", got, want)
	}
}

func TestSearchCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := scanResults(ctx, benchEntries(3*chunkSize), scanQuery{Text: "go"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// BenchmarkSearch measures a full search per keystroke. "narrow" matches a
// few hundred entries; "broad" matches almost everything, which is where
// top-K pays off against the naive score-and-sort-everything approach.
func BenchmarkSearch(b *testing.B) {
	for _, n := range []int{100_000, 1_000_000} {
		snap := benchEntries(n)
		for _, tc := range []struct{ name, text string }{
			{"narrow", "intfile9"},
			{"broad", "go"},
		} {
			q := scanQuery{Text: tc.text}
			b.Run(fmt.Sprintf("n=%d/%s/search", n, tc.name), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, _, err := scanResults(context.Background(), snap, q); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run(fmt.Sprintf("n=%d/%s/naive", n, tc.name), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					naiveSearch(snap, q)
				}
			})
		}
	}
}

// TestSearchBoostNeedsName: a habit lifts what the query is about, not
// what lies below it. "fnd" in ~/dev is about the project fndr: opener,
// visited every day, matches only through its folder fndr, and stays
// below it. Typed for by its own name, opener is lifted as before.
func TestSearchBoostNeedsName(t *testing.T) {
	snap := []indexer.Entry{
		indexer.NewEntry("/r/dev/fndr", 0, 0, true),
		indexer.NewEntry("/r/dev/fndr/internal/opener", 0, 0, true),
		indexer.NewEntry("/r/dev/fndr/internal/openers", 0, 0, true),
	}
	boost := map[string]int{"/r/dev/fndr/internal/opener": 100}
	rs, _, _ := scanResults(context.Background(), snap, scanQuery{Text: "fnd", Under: "/r/dev", Boost: boost})
	if ps := resultPaths(rs); len(ps) == 0 || ps[0] != "/r/dev/fndr" {
		t.Errorf("fnd: %q, want fndr first", ps)
	}
	rs, _, _ = scanResults(context.Background(), snap, scanQuery{Text: "open", Under: "/r/dev", Boost: boost})
	if ps := resultPaths(rs); len(ps) == 0 || ps[0] != "/r/dev/fndr/internal/opener" {
		t.Errorf("open: %q, want the habit, opener, first", ps)
	}
}

// entries builds file entries for the given paths.
func entries(paths ...string) []indexer.Entry {
	es := make([]indexer.Entry, len(paths))
	for i, p := range paths {
		es[i] = indexer.NewEntry(p, 0, 0, false)
	}
	return es
}

func scanResults(ctx context.Context, snap []indexer.Entry, q scanQuery) (results []Result, total int, err error) {
	results, total, _, err = scan(ctx, snap, q, runtime.GOMAXPROCS(0), false)
	return results, total, err
}

func scanMatches(ctx context.Context, snap []indexer.Entry, q scanQuery) (results []Result, total int, matched []int32, err error) {
	return scan(ctx, snap, q, runtime.GOMAXPROCS(0), true)
}
