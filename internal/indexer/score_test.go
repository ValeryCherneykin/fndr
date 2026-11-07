package indexer

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// score is a test helper around fuzzyScore that fails the test on no match.
func score(t *testing.T, path, query string) int {
	t.Helper()
	e := NewEntry(path, 0, 0, false)
	s, ok := fuzzyScore(&e, 0, strings.ToLower(query), nil)
	if !ok {
		t.Fatalf("fuzzyScore(%q, %q): no match", path, query)
	}
	return s
}

// TestFuzzyScoreRanking pins down the ranking rules as orderings rather than
// exact numbers, so the weights can be tuned without rewriting the tests.
func TestFuzzyScoreRanking(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		better, worse string
	}{
		{"compact beats scattered", "foo", "/a/foo.txt", "/a/fxoxo.txt"},
		{"file name beats directory", "main", "/p/main.go", "/main/p.go"},
		{"segment start beats middle", "go", "/a/go.mod", "/a/ago.txt"},
		{"word start beats middle", "test", "/a/my_test.go", "/a/latest.go"},
		{"camelCase hump beats middle", "b", "/a/fooBar.go", "/a/fobar.go"},
		{"short gap beats long gap", "ab", "/x/a_b.txt", "/x/a____b.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, w := score(t, tt.better, tt.query), score(t, tt.worse, tt.query)
			if b <= w {
				t.Errorf("query %q: %q scored %d, %q scored %d; want the first higher",
					tt.query, tt.better, b, tt.worse, w)
			}
		})
	}
}

func TestFuzzyScorePositions(t *testing.T) {
	tests := []struct {
		path, query string
		want        []int
	}{
		{"/src/main.go", "mngo", []int{5, 8, 10, 11}},
		// Pass one starts in the file name, not in /go/.
		{"/go/src/main.go", "go", []int{13, 14}},
		// Pass two compacts: "go", not the "g" of "go" and the "o" of "mod".
		{"/a/go.mod", "go", []int{3, 4}},
		// Byte offsets, not rune indexes: each Cyrillic letter is 2 bytes.
		{"/Отчёт.pdf", "очт", []int{1, 5, 9}},
	}
	for _, tt := range tests {
		t.Run(tt.path+"/"+tt.query, func(t *testing.T) {
			e := NewEntry(tt.path, 0, 0, false)
			pos := make([]int, utf8.RuneCountInString(tt.query))
			if _, ok := fuzzyScore(&e, 0, strings.ToLower(tt.query), pos); !ok {
				t.Fatal("no match")
			}
			if !slices.Equal(pos, tt.want) {
				t.Errorf("positions = %v, want %v", pos, tt.want)
			}
		})
	}
}

// FuzzFuzzyScore checks the invariants that hold for any input: fuzzyScore
// matches exactly when stage one does, and the positions it reports are
// ascending, in bounds, and point at characters equal to the query's.
func FuzzFuzzyScore(f *testing.F) {
	f.Add("/src/main.go", "mngo")
	f.Add("/Отчёт.pdf", "ОТЧ")
	f.Add("/a/fooBar_baz-qux.go", "fbbq")
	f.Fuzz(func(t *testing.T, path, query string) {
		// The invariants are about valid text; invalid UTF-8 cannot be
		// highlighted meaningfully anyway.
		if !utf8.ValidString(path) || !utf8.ValidString(query) {
			t.Skip()
		}
		e := NewEntry(path, 0, 0, false)
		lq := strings.ToLower(query)
		qr := []rune(lq)
		pos := make([]int, len(qr))

		_, ok := fuzzyScore(&e, 0, lq, pos)
		if want := hasSubsequenceFold(path, lq); ok != want {
			t.Fatalf("fuzzyScore ok = %v, hasSubsequenceFold = %v", ok, want)
		}
		if !ok {
			return
		}
		for i, p := range pos {
			if p < 0 || p >= len(path) || (i > 0 && p <= pos[i-1]) {
				t.Fatalf("positions %v not ascending and in bounds for %q", pos, path)
			}
			r, _ := utf8.DecodeRuneInString(path[p:])
			if foldRune(r) != qr[i] {
				t.Fatalf("pos[%d] = %d points at %q, want %q", i, p, r, qr[i])
			}
		}
	})
}

// BenchmarkFuzzyScore measures stage two per candidate, without positions:
// the cost of ranking every entry that survived stage one.
func BenchmarkFuzzyScore(b *testing.B) {
	es := benchEntries(100_000)
	lq := strings.ToLower("intfile9")
	b.ReportAllocs()
	for b.Loop() {
		for i := range es {
			fuzzyScore(&es[i], 0, lq, nil)
		}
	}
}
