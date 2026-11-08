package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCursorFixesTypoMidQuery is the reason the cursor exists: fix a letter
// in the middle without erasing everything after it.
func TestCursorFixesTypoMidQuery(t *testing.T) {
	m := testModel(t, "/r/main.go")
	typeText(m, "mn.go")

	for range 4 {
		key(m, 'h', tea.ModCtrl)
	}
	typeText(m, "ai")
	if m.query != "main.go" || m.qpos != 3 {
		t.Errorf("query %q, cursor %d; want main.go with the cursor after mai", m.query, m.qpos)
	}
	if got := m.resultPaths(); len(got) != 1 {
		t.Errorf("results %q, want the edit to search", got)
	}
}

func TestCursorStopsAtEnds(t *testing.T) {
	m := testModel(t, "/r/x")
	typeText(m, "ab")

	key(m, tea.KeyRight, 0)
	if m.qpos != 2 {
		t.Errorf("right at the end moved the cursor to %d", m.qpos)
	}
	for range 5 {
		key(m, tea.KeyLeft, 0)
	}
	if m.qpos != 0 {
		t.Errorf("cursor %d after running left, want 0", m.qpos)
	}
	key(m, 'l', tea.ModCtrl)
	if m.qpos != 1 {
		t.Errorf("cursor %d after ctrl+l, want 1", m.qpos)
	}
}

// TestCursorStepsOverWholeRunes: a byte offset in the middle of a Cyrillic
// letter would make the next insert produce invalid UTF-8.
func TestCursorStepsOverWholeRunes(t *testing.T) {
	m := testModel(t, "/r/x")
	typeText(m, "от")

	key(m, tea.KeyLeft, 0)
	typeText(m, "ч")
	if m.query != "очт" {
		t.Errorf("query %q, want очт", m.query)
	}
}

func TestEditingAtCursor(t *testing.T) {
	m := testModel(t, "/r/x")
	typeText(m, "foo bar")
	for range 3 {
		key(m, tea.KeyLeft, 0)
	}

	key(m, tea.KeyBackspace, 0) // erases the space before "bar"
	if m.query != "foobar" {
		t.Errorf("backspace mid-query: %q", m.query)
	}
	key(m, 'u', tea.ModCtrl) // erases up to the cursor, keeps the rest
	if m.query != "bar" || m.qpos != 0 {
		t.Errorf("ctrl+u: %q cursor %d, want bar at 0", m.query, m.qpos)
	}
	key(m, tea.KeyBackspace, 0) // nothing before the cursor: nothing happens
	if m.query != "bar" {
		t.Errorf("backspace at the start: %q", m.query)
	}
}

// TestArrowsNoLongerChangeFolder: left and right edit the query; folders are
// Enter, "-" and backspace.
func TestArrowsNoLongerChangeFolder(t *testing.T) {
	m, _ := browseModel(t, "src")
	cwd := m.cwd
	key(m, tea.KeyLeft, 0)
	key(m, tea.KeyRight, 0)
	if m.cwd != cwd {
		t.Errorf("arrows moved to %q", m.cwd)
	}
}

// TestEnteringFolderFromSearchResetsCursor: entering a found folder clears
// the query; a cursor left behind pointed past its end and crashed View.
func TestEnteringFolderFromSearchResetsCursor(t *testing.T) {
	m, _ := browseModel(t, "")
	typeText(m, "src")
	key(m, tea.KeyEnter, 0)

	if m.query != "" || m.qpos != 0 {
		t.Fatalf("query %q cursor %d after entering a folder, want both reset", m.query, m.qpos)
	}
	_ = m.View() // must not panic
}
