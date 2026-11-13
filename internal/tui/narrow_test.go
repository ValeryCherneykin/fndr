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
		snap := m.current().ix.Snapshot()
		next := m.keyOf(snap, m.query+string(r), false)
		if m.within(next) != nil {
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

// TestNarrowsWhen: narrowing only when nothing but the text changed, and
// the new text goes on from the old.
func TestNarrowsWhen(t *testing.T) {
	snap := []indexer.Entry{indexer.NewEntry("/r/a", 1, 1, false)}
	other := []indexer.Entry{indexer.NewEntry("/r/a", 1, 1, false)}
	base := searchKey{text: "fi", under: "/r", tags: ".go", day: "2026-11-13", snap: &snap[0], n: 1}
	for _, tc := range []struct {
		name string
		edit func(k *searchKey)
		want bool
	}{
		{"a letter more", func(k *searchKey) { k.text = "fil" }, true},
		{"the same text", func(*searchKey) {}, true},
		{"a letter less", func(k *searchKey) { k.text = "f" }, false},
		{"another text", func(k *searchKey) { k.text = "fo" }, false},
		{"another folder", func(k *searchKey) { k.text, k.under = "fil", "/r/b" }, false},
		{"folders only", func(k *searchKey) { k.text, k.dirs = "fil", true }, false},
		{"other tags", func(k *searchKey) { k.text, k.tags = "fil", ".md" }, false},
		{"another day", func(k *searchKey) { k.text, k.day = "fil", "2026-11-14" }, false},
		{"another snapshot", func(k *searchKey) { k.text, k.snap = "fil", &other[0] }, false},
		{"the snapshot grown", func(k *searchKey) { k.text, k.n = "fil", 2 }, false},
	} {
		next := base
		tc.edit(&next)
		if got := base.narrows(next); got != tc.want {
			t.Errorf("%s: narrows %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestNarrowIsUsed: the next key searches only what the last matched. Told
// that nothing matched, it finds nothing; searching the whole index again,
// as it would with the narrowing lost, it would find main.go.
func TestNarrowIsUsed(t *testing.T) {
	m := testModel(t, "/r/a/main.go", "/r/b/notes.md")
	typeText(m, "mai")
	if m.narrow == nil {
		t.Fatal("a search kept nothing to narrow the next")
	}
	m.narrow.matched = []int32{}
	typeText(m, "n")
	if len(m.results) != 0 {
		t.Errorf("results %q: the key searched the whole index again", m.resultPaths())
	}
}
