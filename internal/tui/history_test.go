package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/frecency"
)

func withHistory(t *testing.T, m *Model) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "frecency")
	hist, err := frecency.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m.useHistory(hist)
	return path
}

// TestHabitRanksFirst is the feature in one scenario: of two files with the
// same name, open the one that is not first, and next time it is.
func TestHabitRanksFirst(t *testing.T) {
	m := testModel(t, "/r/a/main.go", "/r/b/main.go")
	withHistory(t, m)

	typeText(m, "main")
	if len(m.results) != 2 {
		t.Fatalf("results %q, want both main.go", m.resultPaths())
	}
	second := m.results[1].Entry.Path
	key(m, tea.KeyDown, 0)
	key(m, tea.KeyEnter, 0) // opening fails (the files are not real); the visit counts

	key(m, 'u', tea.ModCtrl)
	typeText(m, "main")
	if got := m.results[0].Entry.Path; got != second {
		t.Errorf("first result %q, want %q, the one just opened", got, second)
	}
}

// TestEnteringFolderAndJumpingAreLearned: folders count as much as files;
// zoxide's lesson is that the places you go are the habit.
func TestEnteringFolderAndJumpingAreLearned(t *testing.T) {
	m, root := browseModel(t, "")
	withHistory(t, m)

	m.selectName(t, "src")
	key(m, tea.KeyEnter, 0)
	key(m, 'e', tea.ModCtrl) // browsing src: jumps to src

	// Entered and jumped to: two visits within the hour, 2 × weight 4.
	src := filepath.Join(root, "src")
	if got := m.hist.Score(src, time.Now()); got != 8 {
		t.Errorf("score of src = %v, want 8: entered once, jumped to once", got)
	}
	if m.boosts[src] == 0 || m.hist.Len() != 1 {
		t.Errorf("boosts %v, want src and nothing else", m.boosts)
	}
}

func TestSaveHistoryWritesWhatWasLearned(t *testing.T) {
	m := testModel(t, "/r/a.go")
	path := withHistory(t, m)
	typeText(m, "a")
	key(m, tea.KeyEnter, 0)

	if err := m.SaveHistory(); err != nil {
		t.Fatal(err)
	}
	back, err := frecency.Open(path)
	if err != nil || back.Len() != 1 {
		t.Errorf("saved history has %v paths (err %v), want the one opened", back.Len(), err)
	}
}

// TestNoHistoryStillWorks: without a history (it could not be opened)
// fndr does everything as before, it just does not learn.
func TestNoHistoryStillWorks(t *testing.T) {
	m := testModel(t, "/r/a.go")
	typeText(m, "a")
	key(m, tea.KeyEnter, 0)
	if err := m.SaveHistory(); err != nil {
		t.Errorf("SaveHistory without a history: %v", err)
	}
}
