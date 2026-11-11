package frecency

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state", "frecency"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWeight(t *testing.T) {
	tests := []struct {
		age  time.Duration
		want float64
	}{
		{0, 4},
		{59 * time.Minute, 4},
		{time.Hour, 2},
		{23 * time.Hour, 2},
		{24 * time.Hour, 0.5},
		{6 * 24 * time.Hour, 0.5},
		{7 * 24 * time.Hour, 0.25},
		{365 * 24 * time.Hour, 0.25},
	}
	for _, tt := range tests {
		if got := weight(tt.age); got != tt.want {
			t.Errorf("weight(%v) = %v, want %v", tt.age, got, tt.want)
		}
	}
}

// TestRecentBeatsFrequentButOld is the point of frecency, in one example.
func TestRecentBeatsFrequentButOld(t *testing.T) {
	s := open(t)
	halfYearAgo := now.Add(-180 * 24 * time.Hour)
	for range 80 {
		s.Visit("/old-project", halfYearAgo)
	}
	for range 50 {
		s.Visit("/dev", now.Add(-2*time.Hour))
	}

	old, dev := s.Score("/old-project", now), s.Score("/dev", now)
	if dev <= old {
		t.Errorf("score /dev = %v, /old-project = %v; want the recent habit first", dev, old)
	}
	if s.Score("/never", now) != 0 {
		t.Error("a path never visited has a score")
	}
}

// TestSaveAndOpen: what is saved comes back, including a path with spaces
// and a tab, which the line format must survive.
func TestSaveAndOpen(t *testing.T) {
	s := open(t)
	odd := "/Users/v/My Files/a\tb"
	s.Visit("/dev", now)
	s.Visit("/dev", now)
	s.Visit(odd, now.Add(-time.Hour))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	back, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Len() != 2 || back.Score("/dev", now) != s.Score("/dev", now) || back.Score(odd, now) != s.Score(odd, now) {
		t.Errorf("reopened store has %d paths, scores %v %v; want what was saved",
			back.Len(), back.Score("/dev", now), back.Score(odd, now))
	}
}

// TestSaveAfterADroppedFlush: a Flush whose Write never runs (the program
// quit before its command did) must not hang Save. It did: ctrl+e quit
// fndr and the shell waited forever.
func TestSaveAfterADroppedFlush(t *testing.T) {
	s := open(t)
	s.Visit("/dev", now)
	_ = s.Flush() // handed out, never written

	saved := make(chan error)
	go func() { saved <- s.Save() }()
	select {
	case err := <-saved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Save waits for a write that nothing will run")
	}
	back, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Len() != 1 {
		t.Errorf("reopened store has %d paths, want the visit Save wrote", back.Len())
	}
}

func TestOpenMissingIsEmpty(t *testing.T) {
	s := open(t)
	if s.Len() != 0 {
		t.Errorf("a store that was never saved has %d paths", s.Len())
	}
	if err := s.Save(); err != nil { // nothing changed: nothing written
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Error("Save wrote a file with nothing to remember")
	}
}

// TestDamagedLinesAreSkipped: a hand-edited or half-written history costs
// its bad lines, never the start-up.
func TestDamagedLinesAreSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency")
	lines := []string{
		"3\t1762689600\t/good",
		"garbage",
		"x\t1762689600\t/bad-count",
		"2\tnot-a-time\t/bad-time",
		"-1\t1762689600\t/negative",
		"",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 1 || s.Score("/good", now) == 0 {
		t.Errorf("loaded %d paths, want only /good", s.Len())
	}
}

// TestAgingForgetsOldHabits: past maxTotal every count shrinks, and paths
// visited once long ago drop out, so the file stays small forever. Aging
// happens as a save merges, on the whole file.
func TestAgingForgetsOldHabits(t *testing.T) {
	entries := map[string]*entry{"/once": {count: 1, last: now}}
	merge(entries, map[string]*entry{"/daily": {count: maxTotal, last: now}})

	if entries["/once"] != nil {
		t.Error("a single visit survived aging")
	}
	total := 0.0
	for _, e := range entries {
		total += e.count
	}
	if total > maxTotal || entries["/daily"] == nil {
		t.Errorf("after aging: total %v, entries %v", total, entries)
	}
}

func TestPathsTheFormatCannotHoldAreIgnored(t *testing.T) {
	s := open(t)
	s.Visit("/a\nb", now)
	s.Visit("", now)
	if s.Len() != 0 {
		t.Errorf("stored %d unstorable paths", s.Len())
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg")
	if p, _ := DefaultPath(); p != "/xdg/fndr/frecency" {
		t.Errorf("with XDG_STATE_HOME: %q", p)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/v")
	if p, _ := DefaultPath(); p != "/home/v/.local/state/fndr/frecency" {
		t.Errorf("without XDG_STATE_HOME: %q", p)
	}
}

func TestBoosts(t *testing.T) {
	s := open(t)
	for range 20 {
		s.Visit("/dev", now)
	}
	s.Visit("/once", now.Add(-365*24*time.Hour))

	b := s.Boosts(now)
	if b["/dev"] <= b["/once"] || b["/never"] != 0 {
		t.Errorf("boosts %v: want the habit above the one-off, nothing for the unknown", b)
	}

	// The map is a snapshot: later visits must not change it under a
	// search that is reading it.
	before := b["/dev"]
	s.Visit("/dev", now)
	if b["/dev"] != before {
		t.Error("a visit changed a snapshot already handed out")
	}
}

// TestBoostScale: the first visits matter most, and even a huge habit stays
// in the range of a few good fuzzy matches.
func TestBoostScale(t *testing.T) {
	if boost(0) != 0 {
		t.Errorf("boost(0) = %d, want 0", boost(0))
	}
	if b1, b2 := boost(4), boost(8); b2-b1 >= b1 {
		t.Errorf("boost(4) = %d, boost(8) = %d: doubling should add less than the first visits did", b1, b2)
	}
	if b := boost(4000); b < 100 || b > 130 {
		t.Errorf("boost(4000) = %d, want about 120", b)
	}
}

// BenchmarkBoosts rebuilds the boost map of a store with 5000 paths, more
// than aging lets a store keep in practice. It runs after every visit.
func BenchmarkBoosts(b *testing.B) {
	s, t := bigStore(5000)
	for b.Loop() {
		s.Boosts(t)
	}
}

// BenchmarkTop picks the habits of a folder, as every folder listing does.
func BenchmarkTop(b *testing.B) {
	s, t := bigStore(5000)
	for b.Loop() {
		s.Top("/home/v/dev", 5, t)
	}
}

func bigStore(n int) (*Store, time.Time) {
	s := &Store{entries: make(map[string]*entry, n)}
	for i := range n {
		p := fmt.Sprintf("/home/v/dev/project%d/internal/pkg%d", i%50, i)
		s.entries[p] = &entry{count: float64(1 + i%40), last: now.Add(-time.Duration(i) * time.Minute)}
	}
	return s, now
}

// TestRename: a renamed folder keeps its habits, and so does everything
// below it; a path that only starts the same ("/a/bc") is not below it.
func TestRename(t *testing.T) {
	s := open(t)
	for _, p := range []string{"/a/b", "/a/b", "/a/b/x.go", "/a/bc", "/c"} {
		s.Visit(p, now)
	}
	s.Visit("/c", now)
	s.Rename(Move{"/a/b", "/c"}) // onto a remembered path: the visits add up
	for p, want := range map[string]float64{"/a/b": 0, "/a/b/x.go": 0, "/c": 4 * 4, "/c/x.go": 4, "/a/bc": 4} {
		if got := s.Score(p, now); got != want {
			t.Errorf("score of %s = %v, want %v", p, got, want)
		}
	}
}

// TestRenameReachesTheFile: the rename is saved, and opening the file again
// finds the habits under the new name only.
func TestRenameReachesTheFile(t *testing.T) {
	s := open(t)
	s.Visit("/old", now)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s.Rename(Move{"/old", "/new"})
	if !s.Pending() {
		t.Fatal("a rename alone is nothing to save")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Score("/old", now) != 0 || r.Score("/new", now) == 0 {
		t.Errorf("in the file: old %v, new %v; want the habit moved", r.Score("/old", now), r.Score("/new", now))
	}
}

// TestRenameAcrossPanes: one fndr renames, another, opened before, saves
// its own visits later. Its save reads the file again, so it must not bring
// the old name back.
func TestRenameAcrossPanes(t *testing.T) {
	a := open(t)
	a.Visit("/old", now)
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := Open(a.path)
	if err != nil {
		t.Fatal(err)
	}

	a.Rename(Move{"/old", "/new"})
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	b.Visit("/elsewhere", now)
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}

	r, err := Open(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Score("/old", now) != 0 || r.Score("/new", now) == 0 || r.Score("/elsewhere", now) == 0 {
		t.Errorf("old %v, new %v, elsewhere %v; want the rename and b's visit kept", r.Score("/old", now), r.Score("/new", now), r.Score("/elsewhere", now))
	}
	if b.Score("/old", now) != 0 {
		t.Error("b still ranks the old name after its save took in the file")
	}
}

// TestRenameSurvivesAFailedSave: a save that fails hands the rename back,
// and the next one writes it.
func TestRenameSurvivesAFailedSave(t *testing.T) {
	s := open(t)
	s.Visit("/old", now)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s.Rename(Move{"/old", "/new"})

	good := s.path
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "frecency") // under a file: cannot be written
	if err := s.Save(); err == nil {
		t.Fatal("the save did not fail: nothing to test")
	}
	s.path = good
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(good)
	if err != nil {
		t.Fatal(err)
	}
	if r.Score("/old", now) != 0 || r.Score("/new", now) == 0 {
		t.Errorf("old %v, new %v; want the rename written by the second save", r.Score("/old", now), r.Score("/new", now))
	}
}

// visits reads the visits of each path, in memory and in the file, as
// "memory/file".
func visits(t *testing.T, s *Store, paths ...string) map[string]string {
	t.Helper()
	r, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range paths {
		count := func(s *Store) float64 {
			if e := s.entries[p]; e != nil {
				return e.count
			}
			return 0
		}
		got[p] = fmt.Sprintf("%g/%g", count(s), count(r))
	}
	return got
}

// TestRenameThenNameReused: notes renamed to old, a new notes created and
// opened. The new visit is the new notes': it used to follow the rename,
// as the save replayed renames after adding the visits.
func TestRenameThenNameReused(t *testing.T) {
	s := open(t)
	s.Visit("/notes", now)
	s.Rename(Move{"/notes", "/old"})
	s.Visit("/notes", now)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"/notes": "1/1", "/old": "1/1"}
	if got := visits(t, s, "/notes", "/old"); !maps.Equal(got, want) {
		t.Errorf("visits (memory/file) %v, want %v", got, want)
	}
}

// TestRenameSwap: a and b swapped in one plan keep their own habits. Taken
// one after the other, a→b merged them and b→a moved both.
func TestRenameSwap(t *testing.T) {
	s := open(t)
	s.Visit("/a", now)
	s.Visit("/a", now)
	s.Visit("/b", now)
	s.Visit("/a/x", now) // and what is inside goes with the folder
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s.Rename(Move{"/a", "/b"}, Move{"/b", "/a"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"/a": "1/1", "/b": "2/2", "/b/x": "1/1", "/a/x": "0/0"}
	if got := visits(t, s, "/a", "/b", "/b/x", "/a/x"); !maps.Equal(got, want) {
		t.Errorf("visits (memory/file) %v, want %v", got, want)
	}
}

// TestRenameWhileSaving: a rename and a visit made while a save runs, in
// the names of after it, end up where they belong once it lands, whether
// it succeeds or fails.
func TestRenameWhileSaving(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint("fail=", fail), func(t *testing.T) {
			s := open(t)
			s.Visit("/notes", now)
			good := s.path
			if fail {
				blocker := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(blocker, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				s.path = filepath.Join(blocker, "frecency") // under a file: cannot be written
			}
			f := s.Flush()
			s.Rename(Move{"/notes", "/old"})
			s.Visit("/notes", now)
			f.Write() //nolint:errcheck // Saved returns it
			if err := s.Saved(f); (err != nil) != fail {
				t.Fatalf("save error %v, want failing %v", err, fail)
			}
			s.path = good
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"/notes": "1/1", "/old": "1/1"}
			if got := visits(t, s, "/notes", "/old"); !maps.Equal(got, want) {
				t.Errorf("visits (memory/file) %v, want %v", got, want)
			}
		})
	}
}
