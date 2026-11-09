package frecency

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestTwoPanesAddUp is the tmux case: two fndr open on one history, each
// visiting, each saving. Every visit of both must be in the file; before,
// the last to save erased the other's.
func TestTwoPanesAddUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency")
	a, b := mustOpen(t, path), mustOpen(t, path)

	for range 3 {
		a.Visit("/dev", now)
	}
	b.Visit("/dev", now)
	b.Visit("/docs", now)
	for _, s := range []*Store{a, b} {
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}

	file := mustOpen(t, path)
	if got := file.entries["/dev"].count; got != 4 {
		t.Errorf("/dev counted %v times, want 3 + 1", got)
	}
	if file.entries["/docs"] == nil {
		t.Error("/docs, visited only in b, is gone")
	}
	// After saving, b ranks by the visits of both panes, not only its own.
	if got := b.entries["/dev"].count; got != 4 {
		t.Errorf("b sees /dev %v times, want 4", got)
	}
}

// TestSavesNeverCollide: many saves at once, as from many panes, each with
// its own visit. The lock orders them, so all of them count.
func TestSavesNeverCollide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency")
	const panes = 20
	var wg sync.WaitGroup
	for range panes {
		s := mustOpen(t, path)
		s.Visit("/dev", now)
		f := s.Flush()
		wg.Go(func() {
			if err := f.Write(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := mustOpen(t, path).entries["/dev"].count; got != panes {
		t.Errorf("/dev counted %v times, want %d", got, panes)
	}
}

// TestFailedSaveKeepsVisits: a save that cannot write gives its visits
// back, and the next save writes them.
func TestFailedSaveKeepsVisits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "frecency")
	s := mustOpen(t, path)
	s.Visit("/dev", now)

	// A file where the folder should be: the save cannot make its folder.
	if err := os.WriteFile(filepath.Join(dir, "state"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err == nil {
		t.Fatal("save succeeded without its folder")
	}
	if err := os.Remove(filepath.Join(dir, "state")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if got := mustOpen(t, path).entries["/dev"]; got == nil || got.count != 1 {
		t.Errorf("after the retry the file holds %v, want one visit", got)
	}
}

// TestSaveWaitsForFlight: at exit, a save still running is waited for and
// counted once, then what came after it is saved too.
func TestSaveWaitsForFlight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency")
	s := mustOpen(t, path)
	s.Visit("/dev", now)
	f := s.Flush()
	go f.Write() //nolint:errcheck // Save reports it
	if s.Flush() != nil {
		t.Error("a second save started while one was running")
	}
	s.Visit("/docs", now)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	file := mustOpen(t, path)
	if file.entries["/dev"].count != 1 || file.entries["/docs"] == nil {
		t.Errorf("file holds %v", file.entries)
	}
}

func mustOpen(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
