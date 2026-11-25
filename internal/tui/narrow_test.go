package tui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// fullSearch is the results of the current query searched over the whole
// index, the narrowing forgotten.
func fullSearch(m *Model) []string {
	saved := m.narrow
	m.narrow = nil
	send(m, m.search()())
	got := m.resultPaths()
	m.narrow = saved
	return got
}

// TestTypingNarrows: each key typed searches what the last matched, and
// finds exactly what a search of the whole index finds.
func TestTypingNarrows(t *testing.T) {
	// Names that part at the first letter, as names in a home do: x is in
	// a third of them.
	words := []string{"xray", "quux", "zeta"}
	paths := make([]string, 3000)
	for i := range paths {
		paths[i] = fmt.Sprintf("/r/dev/proj%d/pkg%d/%s_%d.go", i%30, i%7, words[i%3], i)
	}
	m := testModel(t, paths...)
	narrowed := 0
	for _, r := range "xray_12" {
		if m.narrow != nil {
			narrowed++
		}
		typeText(m, string(r))
		got := m.resultPaths()
		if want := fullSearch(m); !slices.Equal(got, want) {
			t.Fatalf("%q: narrowed %q\nwant %q", m.query, got, want)
		}
	}
	if narrowed < 5 {
		t.Errorf("narrowed on %d keys of 7: the narrowing is not used", narrowed)
	}
}

// TestNarrowSeesNewEntries: a crawl adds to the index between two keys;
// the next search is of the whole index, or what was added is missed.
func TestNarrowSeesNewEntries(t *testing.T) {
	m := testModel(t, "/r/a/main.go", "/r/b/notes.md")
	typeText(m, "mai")
	m.local.ix.Add([]indexer.Entry{indexer.NewEntry("/r/c/main_test.go", 1, 1, false)})
	typeText(m, "n")
	if !slices.Contains(m.resultPaths(), "/r/c/main_test.go") {
		t.Errorf("results %q: the entry added between keys is missed", m.resultPaths())
	}
}
