package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newItem types name after ctrl+o and presses enter.
func newItem(m *Model, name string) {
	key(m, 'o', tea.ModCtrl)
	typeText(m, name)
	send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

func (m *Model) selected() string {
	if len(m.results) == 0 {
		return ""
	}
	return m.results[m.cursor].Entry.Name()
}

// TestCreateShowsAndSelects is the scenario that once failed for real: the
// file was made, but the search on screen hid it. Whatever was typed, the
// new item must be in the list and selected.
func TestCreateShowsAndSelects(t *testing.T) {
	m, root := browseModel(t, "")
	typeText(m, "zzz") // a search that matches nothing
	newItem(m, "notes.md")

	if _, err := os.Stat(filepath.Join(root, "notes.md")); err != nil {
		t.Fatal(err)
	}
	if m.naming || m.query != "" {
		t.Errorf("naming %v, query %q: want the folder back", m.naming, m.query)
	}
	if got := m.selected(); got != "notes.md" {
		t.Errorf("selected %q, want notes.md", got)
	}
	if !strings.Contains(m.notice, "created") || m.noticeErr {
		t.Errorf("notice %q", m.notice)
	}
}

// TestCreateHiddenAndNested: a hidden name is still shown, and for
// "a/b/c.txt" the folder a is what gets selected.
func TestCreateHiddenAndNested(t *testing.T) {
	m, _ := browseModel(t, "")
	newItem(m, ".env")
	if got := m.selected(); got != ".env" {
		t.Errorf("selected %q, want .env", got)
	}
	newItem(m, "a/b/c.txt")
	if got := m.selected(); got != "a" {
		t.Errorf("selected %q, want a", got)
	}
}

// TestCreateIsSearchable: the index learns new items at once, each level
// once, without a new crawl.
func TestCreateIsSearchable(t *testing.T) {
	m, _ := browseModel(t, "")
	newItem(m, "src/deep/quux.go") // src is indexed already: not added twice
	typeText(m, "quux")
	if got := m.names(); len(got) != 1 || got[0] != "quux.go" {
		t.Errorf("search found %q, want quux.go", got)
	}
	send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	typeText(m, "src/")
	n := 0
	for _, name := range m.names() {
		if name == "src" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("src/ found %q, want src once", m.names())
	}
}

// TestCreateErrorKeepsName: a failure is said, and the name stays to be
// fixed rather than retyped.
func TestCreateErrorKeepsName(t *testing.T) {
	m, _ := browseModel(t, "")
	newItem(m, "README.md") // exists
	if !m.naming || m.name != "README.md" {
		t.Errorf("naming %v name %q: want the field kept", m.naming, m.name)
	}
	if !m.noticeErr || !strings.Contains(m.notice, "already exists") {
		t.Errorf("notice %q, want the error", m.notice)
	}
}

// TestNamingLeavesQueryAlone: esc gives the search back as it was, and
// typing a name never searches.
func TestNamingLeavesQueryAlone(t *testing.T) {
	m, _ := browseModel(t, "")
	typeText(m, "main")
	before := m.names()

	key(m, 'o', tea.ModCtrl)
	typeText(m, "x-y")         // "-" is text here, not "go up"
	for range len("x-y") + 1 { // one more than typed: never leaves the folder
		send(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if m.name != "" || !slices.Equal(m.names(), before) {
		t.Errorf("name %q, list %q: want the search untouched", m.name, m.names())
	}
	send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.naming || m.query != "main" || m.qpos != len("main") {
		t.Errorf("after esc: naming %v query %q pos %d", m.naming, m.query, m.qpos)
	}
}

// TestNamingView: the line and the status bar both say NEW.
func TestNamingView(t *testing.T) {
	m, _ := browseModel(t, "")
	key(m, 'o', tea.ModCtrl)
	view := m.View().Content
	for _, want := range []string{" NEW ", "name/ for a folder"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	typeText(m, "ab")
	if c := m.View().Cursor; c.X != promptWidth+2 {
		t.Errorf("cursor at %d, want after the name", c.X)
	}
}
