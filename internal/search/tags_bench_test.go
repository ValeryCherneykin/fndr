package search

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ValeryCherneykin/fndr/internal/filter"
	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

const under = "/home/me"

// BenchmarkSearchTags: what tags cost a whole search over 100k entries, as
// the TUI makes it: with a query (tags only see what it found), and with
// tags alone (every entry is tested, and ranked by time).
func BenchmarkSearchTags(b *testing.B) {
	es := benchIndex()
	code, err := filter.Parse("code|doc")
	if err != nil {
		b.Fatal(err)
	}
	keep := filter.Stack{code}.Keep(es, under, time.Now())
	for _, q := range []scanQuery{
		{Text: "pkgmain", Under: under},
		{Text: "pkgmain", Under: under, Keep: keep},
		{Under: under, Keep: keep},
	} {
		name := fmt.Sprintf("text=%q/tags=%t", q.Text, q.Keep != nil)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := scanResults(context.Background(), es, q); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchIndex() []indexer.Entry {
	kinds := []string{"main.go", "README.md", "photo.jpg", "notes.txt", "lib.rs", "archive.zip", "Makefile"}
	es := make([]indexer.Entry, 100_000)
	for i := range es {
		es[i] = indexer.NewEntry(fmt.Sprintf("%s/dev/project%d/pkg/%s", under, i%300, kinds[i%len(kinds)]), int64(i*37), int64(i), false)
	}
	return es
}
