package frecency

import (
	"slices"
	"testing"
	"time"
)

func TestTop(t *testing.T) {
	s := open(t)
	visit := func(path string, times int, at time.Time) {
		for range times {
			s.Visit(path, at)
		}
	}
	visit("/home/v/dev", 10, now)
	visit("/home/v/dev/fndr", 30, now)
	visit("/home/v/notes.md", 5, now)
	visit("/home/v/old", 1, now.Add(-60*24*time.Hour)) // a one-off long ago: noise
	visit("/home/vera/x", 50, now)                     // shares the prefix "/home/v", not below it
	visit("/home/v", 50, now)                          // the folder itself is not below itself

	got := s.Top("/home/v", 10, now)
	want := []string{"/home/v/dev/fndr", "/home/v/dev", "/home/v/notes.md"}
	if !slices.Equal(got, want) {
		t.Errorf("Top = %q, want %q", got, want)
	}
	if got := s.Top("/home/v", 2, now); !slices.Equal(got, want[:2]) {
		t.Errorf("Top(n=2) = %q, want %q", got, want[:2])
	}
	if got := s.Top("/", 1, now); len(got) != 1 {
		t.Errorf("Top under / = %q, want one path", got)
	}
}

// TestTopTiesAreStable: equal scores come back in the same order every
// time, though map iteration order is random.
func TestTopTiesAreStable(t *testing.T) {
	s := open(t)
	for _, p := range []string{"/r/c", "/r/a", "/r/b"} {
		s.Visit(p, now)
	}
	for range 20 {
		if got := s.Top("/r", 3, now); !slices.Equal(got, []string{"/r/a", "/r/b", "/r/c"}) {
			t.Fatalf("Top = %q, want ties ordered by path", got)
		}
	}
}
