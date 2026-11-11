package tui

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/frecency"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
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

// habitModel is browseModel with a history in which src/main.go is a strong
// habit and docs a weaker one, loaded the way a start-up would load it.
func habitModel(t *testing.T) (m *Model, root string) {
	t.Helper()
	m, root = browseModel(t, "")
	withHistory(t, m)
	now := time.Now()
	for range 5 {
		m.hist.Visit(filepath.Join(root, "src", "main.go"), now)
	}
	m.hist.Visit(filepath.Join(root, "docs"), now)
	m.hist.Visit(filepath.Join(root, "gone"), now) // deleted since: must not be offered
	send(m, m.load(m.cwd)())
	return m, root
}

// TestHabitsComeFirst: open fndr, and the places you go are on top, the
// strongest selected: Enter takes you there.
func TestHabitsComeFirst(t *testing.T) {
	m, root := habitModel(t)

	want := []string{"main.go", "docs", "docs", "src", "README.md", ".git"}
	if got := m.names(); !slices.Equal(got, want) {
		t.Errorf("list %q, want habits (main.go, docs) above the contents", got)
	}
	if m.habits != 2 || m.cursor != 0 {
		t.Errorf("habits %d, cursor %d; want 2 habits and the first selected", m.habits, m.cursor)
	}
	if m.selectedPath() != filepath.Join(root, "src", "main.go") {
		t.Errorf("selected %q, want the strongest habit", m.selectedPath())
	}
}

// TestHabitsOnlyWhileBrowsing: a query shows search results, no habit rows.
func TestHabitsOnlyWhileBrowsing(t *testing.T) {
	m, _ := habitModel(t)
	typeText(m, "read")
	if got := m.names(); !slices.Equal(got, []string{"README.md"}) {
		t.Errorf("search shows %q, want just the match", got)
	}
	key(m, tea.KeyBackspace, 0)
	key(m, tea.KeyBackspace, 0)
	key(m, tea.KeyBackspace, 0)
	key(m, tea.KeyBackspace, 0)
	if m.habits != 2 || m.names()[0] != "main.go" {
		t.Errorf("back to browsing: %q, want the habits again", m.names())
	}
}

// TestRevealSkipsHabits: coming back up out of docs selects docs in the
// folder's contents, not the docs habit row above them.
func TestRevealSkipsHabits(t *testing.T) {
	m, _ := habitModel(t)
	m.cursor = 2 // docs, in the contents
	key(m, tea.KeyEnter, 0)
	key(m, tea.KeyBackspace, 0)
	if m.cursor != 2 {
		t.Errorf("cursor %d after coming back up, want 2: docs in the contents", m.cursor)
	}
}

// TestVisitSavedAtOnce: entering a folder writes the history right away,
// so closing the terminal, which kills fndr before any exit code, loses
// nothing.
func TestVisitSavedAtOnce(t *testing.T) {
	m, root := browseModel(t, "")
	path := withHistory(t, m)
	m.selectName(t, "src")
	send(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // runs the save, as the runtime would

	saved, err := frecency.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Score(filepath.Join(root, "src"), time.Now()) == 0 {
		t.Error("the visit is not in the file before exit")
	}
	if m.hist.Pending() || m.noticeErr {
		t.Errorf("pending %v, notice %q", m.hist.Pending(), m.notice)
	}
}

// TestRenameKeepsTheHabit: a file moved with ctrl+r keeps its rank under
// the new name, and the file of the history says so.
func TestRenameKeepsTheHabit(t *testing.T) {
	m, root := renameModel(t)
	path := withHistory(t, m)
	from, to := filepath.Join(root, "src", "util.go"), filepath.Join(root, "lib", "util.go")
	m.hist.Visit(from, time.Now())

	retype(m, "lib/util.go")
	if m.hist.Score(from, time.Now()) != 0 || m.hist.Score(to, time.Now()) == 0 {
		t.Errorf("score old %v, new %v; want the habit moved", m.hist.Score(from, time.Now()), m.hist.Score(to, time.Now()))
	}
	if m.boosts[to] == 0 {
		t.Error("search does not rank the new name yet")
	}
	saved, err := frecency.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Score(to, time.Now()) == 0 {
		t.Error("the move was not saved")
	}
}

// TestSwapKeepsBothHabits: a and b swapped with one :w are two renames
// made at once, and handed to the history at once. One by one, a→b would
// merge the two habits and b→a move them both.
func TestSwapKeepsBothHabits(t *testing.T) {
	m, root := renameModel(t)
	withHistory(t, m)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	now := time.Now()
	m.hist.Visit(a, now)
	m.hist.Visit(a, now)
	m.hist.Visit(b, now)
	before := map[string]float64{a: m.hist.Score(a, now), b: m.hist.Score(b, now)}

	m.relearn(root, mutation.Plan{{Kind: mutation.OpRename, From: "a", To: "b"}, {Kind: mutation.OpRename, From: "b", To: "a"}})
	if m.hist.Score(b, now) != before[a] || m.hist.Score(a, now) != before[b] {
		t.Errorf("a %v, b %v; want them swapped from a %v, b %v", m.hist.Score(a, now), m.hist.Score(b, now), before[a], before[b])
	}
}
