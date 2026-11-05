package indexer

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

func TestIndexZeroValue(t *testing.T) {
	var ix Index
	if ix.Len() != 0 || len(ix.Snapshot()) != 0 {
		t.Error("zero Index must be empty")
	}
}

func TestIndexAddAndSnapshot(t *testing.T) {
	var ix Index
	ix.Add([]Entry{NewEntry("/a", 0, 0, false), NewEntry("/b", 0, 0, false)})
	ix.Add([]Entry{NewEntry("/c", 0, 0, false)})

	snap := ix.Snapshot()
	if len(snap) != 3 || ix.Len() != 3 {
		t.Fatalf("len(Snapshot) = %d, Len = %d, want 3", len(snap), ix.Len())
	}
	for i, want := range []string{"/a", "/b", "/c"} {
		if snap[i].Path != want {
			t.Errorf("snap[%d].Path = %q, want %q", i, snap[i].Path, want)
		}
	}
}

func TestIndexSnapshotIsStable(t *testing.T) {
	var ix Index
	ix.Add([]Entry{NewEntry("/a", 0, 0, false)})
	snap := ix.Snapshot()

	ix.Add([]Entry{NewEntry("/b", 0, 0, false)})

	if len(snap) != 1 || snap[0].Path != "/a" {
		t.Errorf("an old snapshot changed after Add: %v", snap)
	}
}

func TestIndexAppendToSnapshotDoesNotLeak(t *testing.T) {
	var ix Index
	// Three single appends grow the capacity 1 -> 2 -> 4, leaving one spare
	// slot after the third entry: the slot a careless Snapshot would expose.
	for _, p := range []string{"/a", "/b", "/c"} {
		ix.Add([]Entry{NewEntry(p, 0, 0, false)})
	}
	if cap(ix.entries) == len(ix.entries) {
		t.Fatal("test setup: no spare capacity, so the test would prove nothing")
	}

	snap := ix.Snapshot()
	ix.Add([]Entry{NewEntry("/d", 0, 0, false)}) // fills the spare slot
	_ = append(snap, NewEntry("/evil", 0, 0, false))

	if got := ix.Snapshot()[3].Path; got != "/d" {
		t.Errorf("entry 3 = %q, want /d: appending to a snapshot overwrote the index", got)
	}
}

// TestIndexConcurrentAddAndSnapshot is meant for `go test -race`: one
// goroutine plays the crawler, another plays a user typing.
func TestIndexConcurrentAddAndSnapshot(t *testing.T) {
	const batches, perBatch = 200, 50
	var ix Index
	var wg sync.WaitGroup

	wg.Go(func() {
		for b := range batches {
			batch := make([]Entry, perBatch)
			for i := range batch {
				batch[i] = NewEntry(fmt.Sprintf("/f%d", b*perBatch+i), 0, 0, false)
			}
			ix.Add(batch)
		}
	})

	wg.Go(func() {
		for range batches {
			// Every snapshot must be a consistent prefix: entry i is /f<i>.
			snap := ix.Snapshot()
			for i, e := range snap {
				if want := fmt.Sprintf("/f%d", i); e.Path != want {
					t.Errorf("snapshot[%d] = %q, want %q", i, e.Path, want)
					return
				}
			}
		}
	})

	wg.Wait()
	if ix.Len() != batches*perBatch {
		t.Errorf("Len = %d, want %d", ix.Len(), batches*perBatch)
	}
}

// entries builds file entries for the given paths.
func entries(paths ...string) []Entry {
	es := make([]Entry, len(paths))
	for i, p := range paths {
		es[i] = NewEntry(p, 0, 0, false)
	}
	return es
}

// paths returns the paths of es, for comparing whole snapshots at once.
func paths(es []Entry) []string {
	ps := make([]string, len(es))
	for i := range es {
		ps[i] = es[i].Path
	}
	return ps
}

func TestIndexUpdate(t *testing.T) {
	base := []string{"/", "/a", "/a/foo", "/a/foo/x.go", "/a/foobar", "/a/foo.go", "/b"}

	tests := []struct {
		name   string
		remove []string
		add    []string
		want   []string
	}{
		{
			name: "create appends",
			add:  []string{"/a/new.go"},
			want: []string{"/", "/a", "/a/foo", "/a/foo/x.go", "/a/foobar", "/a/foo.go", "/b", "/a/new.go"},
		},
		{
			name:   "delete file",
			remove: []string{"/a/foo.go"},
			want:   []string{"/", "/a", "/a/foo", "/a/foo/x.go", "/a/foobar", "/b"},
		},
		{
			// The case a naive strings.HasPrefix gets wrong: /a/foobar and
			// /a/foo.go share the prefix /a/foo but are siblings, not children.
			name:   "delete dir keeps siblings sharing its prefix",
			remove: []string{"/a/foo"},
			want:   []string{"/", "/a", "/a/foobar", "/a/foo.go", "/b"},
		},
		{
			name:   "rename dir",
			remove: []string{"/a/foo"},
			add:    []string{"/a/bar", "/a/bar/x.go"},
			want:   []string{"/", "/a", "/a/foobar", "/a/foo.go", "/b", "/a/bar", "/a/bar/x.go"},
		},
		{
			name:   "delete several",
			remove: []string{"/a", "/b"},
			want:   []string{"/"},
		},
		{
			name:   "delete root removes everything",
			remove: []string{"/"},
			want:   []string{},
		},
		{
			name:   "delete missing path is a no-op",
			remove: []string{"/nope"},
			want:   base,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ix Index
			ix.Add(entries(base...))

			ix.Update(tt.remove, entries(tt.add...))

			if got := paths(ix.Snapshot()); !slices.Equal(got, tt.want) {
				t.Errorf("after Update(%q, %q):\n got  %q\n want %q", tt.remove, tt.add, got, tt.want)
			}
		})
	}
}

func TestIndexUpdateKeepsOldSnapshots(t *testing.T) {
	var ix Index
	ix.Add(entries("/a", "/b", "/c"))
	before := ix.Snapshot()

	ix.Update([]string{"/b"}, entries("/d"))

	if got, want := paths(before), []string{"/a", "/b", "/c"}; !slices.Equal(got, want) {
		t.Errorf("snapshot taken before Update changed: got %q, want %q", got, want)
	}
}

// TestIndexConcurrentUpdate runs crawler, file operations and search at once,
// mainly to give `go test -race` a chance to catch unsynchronized access.
func TestIndexConcurrentUpdate(t *testing.T) {
	var ix Index
	var wg sync.WaitGroup

	wg.Go(func() {
		for i := range 200 {
			ix.Add(entries(fmt.Sprintf("/crawl/%d", i)))
		}
	})
	wg.Go(func() {
		for i := range 200 {
			p := fmt.Sprintf("/user/%d", i)
			ix.Update(nil, entries(p))
			ix.Update([]string{p}, nil)
		}
	})
	wg.Go(func() {
		for range 200 {
			for _, e := range ix.Snapshot() {
				_ = e.Name()
			}
		}
	})

	wg.Wait()
	if n := ix.Len(); n != 200 {
		t.Errorf("Len = %d, want 200: every /user entry was removed again", n)
	}
}

// BenchmarkIndexUpdate measures a delete on a large index: the pause that
// Snapshot, and so typing, may hit while a file operation runs.
func BenchmarkIndexUpdate(b *testing.B) {
	const n = 1_000_000
	es := make([]Entry, n)
	for i := range es {
		es[i] = NewEntry(fmt.Sprintf("/home/user/project%d/src/file%d.go", i%1000, i), 0, 0, false)
	}

	for b.Loop() {
		b.StopTimer()
		var ix Index
		ix.Add(es)
		b.StartTimer()

		ix.Update([]string{"/home/user/project7"}, nil)
	}
}
