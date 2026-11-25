package search

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ValeryCherneykin/fndr/internal/filter"
	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

func TestRequestModesAndLimit(t *testing.T) {
	snap := entries("/r/src/File_7_99.go", "/r/src/file_70_99.go", "/r/src/file_7_909.go", "/r/src/File_7_99.md", "/other/File_7_99.go")
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	r := Request{Query: "file_7_99", Under: "/r", Tags: parseTags(t, ".go"), Limit: 1}
	got := runRequest(t, r, snap, now)
	if got.Total != 3 || len(got.Results) != 1 || got.Results[0].Entry.Path != snap[0].Path {
		t.Fatalf("fuzzy results = %+v", got)
	}
	r.Mode = Contiguous
	whole := runRequest(t, r, snap, now)
	if whole.Total != 1 || !reflect.DeepEqual(whole.Results, got.Results) {
		t.Fatalf("contiguous results = %+v, fuzzy results = %+v", whole, got)
	}
}

func TestRequestDirsAndHas(t *testing.T) {
	snap := []indexer.Entry{
		indexer.NewEntry("/r/project", 0, 0, true),
		indexer.NewEntry("/r/project/src", 0, 0, true),
		indexer.NewEntry("/r/project/src/main.go", 0, 0, false),
		indexer.NewEntry("/r/project/README.md", 0, 0, false),
		indexer.NewEntry("/r/other.go", 0, 0, false),
		indexer.NewEntry("/outside/project", 0, 0, true),
		indexer.NewEntry("/outside/project/main.go", 0, 0, false),
		indexer.NewEntry("/outside/project/README.md", 0, 0, false),
	}
	r := Request{Query: "prj/", Under: "/r", Tags: parseTags(t, "has:.go", "has:.md")}
	got := runRequest(t, r, snap, time.Time{})
	if got.Total != 1 || len(got.Results) != 1 || got.Results[0].Entry.Path != "/r/project" {
		t.Fatalf("has: and / results = %+v", got)
	}
	r.Query, r.Tags = "other/", nil
	if got := runRequest(t, r, snap, time.Time{}); got.Total != 0 {
		t.Fatalf("trailing slash included a file: %+v", got)
	}
}

func TestNarrowingCompatibility(t *testing.T) {
	var ix indexer.Index
	ix.Add(entries("/r/a/main.go", "/r/b/notes.md"))
	snap := ix.Snapshot()
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	r := Request{Query: "mai", Under: "/r", Tags: parseTags(t, ".go"), TrackMatches: true}
	first := runRequest(t, r, snap, now)
	if first.Narrowing == nil {
		t.Fatal("first response has no candidates")
	}

	r.Query, r.Previous = "main", first.Narrowing
	text, dirs := splitQuery(r.Query)
	if got := r.Previous.within(keyFor(snap, r, text, dirs, now)); !slices.Equal(got, []int32{0}) {
		t.Fatalf("reused positions = %v, want [0]", got)
	}

	ix.Update([]string{"/r/b/notes.md"}, entries("/r/b/main.go"))
	for _, tc := range []struct {
		name string
		req  Request
		snap []indexer.Entry
		now  time.Time
	}{
		{"same-length replacement", r, ix.Snapshot(), now},
		{"next day", r, snap, now.AddDate(0, 0, 1)},
		{"deleted characters", Request{Query: "ma", Under: r.Under, Tags: r.Tags}, snap, now},
		{"different text", Request{Query: "mood", Under: r.Under, Tags: r.Tags}, snap, now},
		{"different scope", Request{Query: r.Query, Under: "/r/a", Tags: r.Tags}, snap, now},
		{"directories only", Request{Query: r.Query + "/", Under: r.Under, Tags: r.Tags}, snap, now},
		{"different tags", Request{Query: r.Query, Under: r.Under, Tags: parseTags(t, ".md")}, snap, now},
		{"contiguous mode", Request{Query: r.Query, Under: r.Under, Tags: r.Tags, Mode: Contiguous}, snap, now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, dirs := splitQuery(tc.req.Query)
			if got := first.Narrowing.within(keyFor(tc.snap, tc.req, text, dirs, tc.now)); got != nil {
				t.Fatalf("incompatible request reused candidates %v", got)
			}
		})
	}
	if got := runRequest(t, r, ix.Snapshot(), now); got.Total != 2 {
		t.Fatalf("same-length replacement returned %+v, want both files", got)
	}
}

func TestRequestReranksAllCandidates(t *testing.T) {
	snap := entries("/r/a/main.go", "/r/b/main.go", "/r/notes.md")
	r := Request{Query: "mai", Under: "/r", Limit: 1, TrackMatches: true}
	now := time.Time{}
	first := runRequest(t, r, snap, now)
	r.Query, r.Previous = "main", first.Narrowing
	r.Boost = map[string]int{snap[1].Path: 1000}

	narrowed := runRequest(t, r, snap, now)
	r.Previous = nil
	full := runRequest(t, r, snap, now)
	if narrowed.Total != 2 || len(narrowed.Results) != 1 || narrowed.Results[0].Entry.Path != snap[1].Path ||
		full.Total != narrowed.Total || !reflect.DeepEqual(full.Results, narrowed.Results) {
		t.Fatalf("narrowed = %+v, full = %+v", narrowed, full)
	}
}

func TestRequestReusesEmptyCandidateSet(t *testing.T) {
	snap := entries("/r/main.go", "/r/notes.md")
	now := time.Time{}
	r := Request{Query: "mai", TrackMatches: true}
	text, dirs := splitQuery(r.Query)
	// A non-nil empty set is a valid cache hit. Searching the whole
	// snapshot instead would find main.go in this fixture.
	r.Previous = &Narrowing{key: keyFor(snap, r, text, dirs, now), matched: []int32{}}
	r.Query = "main"
	if got := runRequest(t, r, snap, now); got.Total != 0 {
		t.Fatalf("reused empty candidate set returned %+v", got)
	}
}

func TestRequestTimeTagChangesAcrossMidnight(t *testing.T) {
	now := time.Date(2026, time.November, 25, 23, 59, 0, 0, time.UTC)
	snap := []indexer.Entry{
		indexer.NewEntry("/r/old.go", 0, now.AddDate(0, 0, -1).UnixNano(), false),
		indexer.NewEntry("/r/new.go", 0, now.UnixNano(), false),
	}
	r := Request{Tags: parseTags(t, "@yesterday"), TrackMatches: true}
	before := runRequest(t, r, snap, now)
	if before.Narrowing == nil || len(before.Results) != 1 || before.Results[0].Entry.Path != snap[0].Path {
		t.Fatalf("before midnight = %+v", before)
	}

	r.Query, r.Previous = "n", before.Narrowing
	after := runRequest(t, r, snap, now.Add(2*time.Minute))
	if after.Total != 1 || len(after.Results) != 1 || after.Results[0].Entry.Path != snap[1].Path {
		t.Fatalf("after midnight = %+v", after)
	}
}

func TestRequestCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Request{Query: "main/", Tags: parseTags(t, "has:.go"), TrackMatches: true}
	got, err := r.Run(ctx, entries("/r/main/main.go"), time.Time{})
	if !errors.Is(err, context.Canceled) || got.Results != nil || got.Total != 0 || got.Narrowing != nil {
		t.Fatalf("canceled response = %+v, err = %v", got, err)
	}
}

func parseTags(t *testing.T, tags ...string) filter.Stack {
	t.Helper()
	stack := make(filter.Stack, 0, len(tags))
	for _, text := range tags {
		tag, err := filter.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		stack = append(stack, tag)
	}
	return stack
}

func runRequest(t *testing.T, r Request, snap []indexer.Entry, now time.Time) Response {
	t.Helper()
	got, err := r.Run(context.Background(), snap, now)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
