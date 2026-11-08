package frecency

import (
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
// visited once long ago drop out, so the file stays small forever.
func TestAgingForgetsOldHabits(t *testing.T) {
	s := open(t)
	s.Visit("/once", now)
	for range maxTotal {
		s.Visit("/daily", now)
	}
	if s.Score("/once", now) != 0 {
		t.Error("a single old visit survived aging")
	}
	if s.total > maxTotal {
		t.Errorf("total %v after aging, want at most %d", s.total, maxTotal)
	}
	if s.Score("/daily", now) == 0 {
		t.Error("aging forgot the real habit")
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
