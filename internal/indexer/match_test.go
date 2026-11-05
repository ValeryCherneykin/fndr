package indexer

import (
	"fmt"
	"strings"
	"testing"
)

func TestHasSubsequenceFold(t *testing.T) {
	tests := []struct {
		s, query string
		want     bool
	}{
		{"main.go", "mngo", true},
		{"main.go", "main.go", true},
		{"main.go", "mgn", false}, // all letters present, wrong order
		{"main.go", "", true},     // empty query matches everything
		{"", "a", false},
		{"main.go", "main.goo", false}, // query longer than what s offers
		{"Main.GO", "mngo", true},      // case-insensitive
		{"/Users/dev/fndr/cmd/fndr/main.go", "fndrmain", true},
		{"Отчёт.pdf", "отч", true},         // Cyrillic, uppercase in s
		{"мои файлы/Main.go", "мфm", true}, // Cyrillic and ASCII mixed
		{"отчет.pdf", "отчё", false},       // simple folding: ё is not е
		{"日本語/a.txt", "本a", true},          // 3-byte runes
	}
	for _, tt := range tests {
		t.Run(tt.s+"/"+tt.query, func(t *testing.T) {
			got := hasSubsequenceFold(tt.s, strings.ToLower(tt.query))
			if got != tt.want {
				t.Errorf("hasSubsequenceFold(%q, %q) = %v, want %v", tt.s, tt.query, got, tt.want)
			}
		})
	}
}

// naiveSubsequenceFold is the obvious implementation: lowercase both strings,
// then compare runes. It is the reference for the fuzz test and the baseline
// for the benchmark, and it allocates on every call.
func naiveSubsequenceFold(s, query string) bool {
	q := []rune(strings.ToLower(query))
	if len(q) == 0 {
		return true
	}
	qi := 0
	for _, r := range strings.ToLower(s) {
		if r == q[qi] {
			qi++
			if qi == len(q) {
				return true
			}
		}
	}
	return false
}

// FuzzHasSubsequenceFold checks the optimized matcher against the naive one
// on arbitrary input, including invalid UTF-8. Run it with:
//
//	go test -fuzz=FuzzHasSubsequenceFold ./internal/indexer
func FuzzHasSubsequenceFold(f *testing.F) {
	f.Add("main.go", "mngo")
	f.Add("Отчёт.pdf", "ОТЧ")
	f.Add("日本語", "語")
	f.Add("\xff\xfe", "\xff")
	f.Fuzz(func(t *testing.T, s, query string) {
		got := hasSubsequenceFold(s, strings.ToLower(query))
		if want := naiveSubsequenceFold(s, query); got != want {
			t.Errorf("hasSubsequenceFold(%q, %q) = %v, naive = %v", s, query, got, want)
		}
	})
}

// benchPaths returns n realistic-looking paths for search benchmarks.
func benchPaths(n int) []string {
	dirs := []string{"src", "internal", "cmd", "pkg", "docs", "node_modules", "vendor", "testdata"}
	ps := make([]string, n)
	for i := range ps {
		ps[i] = fmt.Sprintf("/Users/valera/dev/project%d/%s/%s/file_%d.go",
			i%50, dirs[i%len(dirs)], dirs[(i/7)%len(dirs)], i)
	}
	return ps
}

// BenchmarkSubsequence compares the optimized matcher with the naive one over
// 100k paths, one query each: the cost of the first search stage per
// keystroke, on one core. Look at the allocs/op column.
func BenchmarkSubsequence(b *testing.B) {
	paths := benchPaths(100_000)
	const query = "intfile9"

	b.Run("optimized", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			lq := strings.ToLower(query)
			for _, p := range paths {
				hasSubsequenceFold(p, lq)
			}
		}
	})
	b.Run("naive", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for _, p := range paths {
				naiveSubsequenceFold(p, query)
			}
		}
	})
}
