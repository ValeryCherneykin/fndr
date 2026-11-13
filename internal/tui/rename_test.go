package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// renameModel is browseModel with "util" searched and RENAME started on
// src/util.go, its path ready to edit.
func renameModel(t *testing.T) (m *Model, root string) {
	t.Helper()
	m, root = browseModel(t, "")
	typeText(m, "util")
	key(m, 'r', tea.ModCtrl)
	if m.rename == nil {
		t.Fatalf("ctrl+r did not start RENAME; notice %q", m.notice)
	}
	if got := m.rename.line.text; got != "src/util.go" {
		t.Fatalf("the line is %q, want the path from fndr's folder, src/util.go", got)
	}
	return m, root
}

// retype replaces the whole line with s and presses enter.
func retype(m *Model, s string) {
	key(m, 'u', tea.ModCtrl) // the cursor starts at the end: ctrl+u erases it all
	typeText(m, s)
	key(m, tea.KeyEnter, 0)
}

// TestRenameMovesWhatSearchFound: a new folder in the path moves the file
// there, making the folder; then the file is found and selected under its
// new name, and the index knows it.
func TestRenameMovesWhatSearchFound(t *testing.T) {
	m, root := renameModel(t)
	retype(m, "lib/helpers.go")

	from, to := filepath.Join(root, "src", "util.go"), filepath.Join(root, "lib", "helpers.go")
	if exists(from) || !exists(to) {
		t.Fatalf("on disk: old there %v, new there %v; want it moved", exists(from), exists(to))
	}
	if m.indexed(from) || !m.indexed(to) {
		t.Errorf("index: old %v, new %v; want the new path only", m.indexed(from), m.indexed(to))
	}
	if m.rename != nil {
		t.Error("RENAME is still on after the move")
	}
	if m.query != "helpers.go" || len(m.results) == 0 || m.results[m.cursor].Entry.Path != to {
		t.Errorf("query %q, results %v; want the new name searched and the file selected", m.query, m.resultPaths())
	}
	if m.notice != "moved util.go → lib/helpers.go" {
		t.Errorf("notice %q, want it to say what moved", m.notice)
	}
}

// TestRenameInPlace: a new name in the same folder is a rename.
func TestRenameInPlace(t *testing.T) {
	m, root := renameModel(t)
	retype(m, "src/tools.go")
	if !exists(filepath.Join(root, "src", "tools.go")) || m.notice != "renamed util.go → tools.go" {
		t.Errorf("notice %q; want src/tools.go renamed", m.notice)
	}
}

// TestRenameEscChangesNothing: esc leaves RENAME with the disk and the
// query as they were.
func TestRenameEscChangesNothing(t *testing.T) {
	m, root := renameModel(t)
	typeText(m, "x")
	key(m, tea.KeyEscape, 0)
	if m.rename != nil || m.query != "util" || !exists(filepath.Join(root, "src", "util.go")) {
		t.Errorf("after esc: rename %v, query %q; want all as before", m.rename != nil, m.query)
	}
}

// TestRenameRefuses: what cannot be done says why and keeps the line to
// fix, the file where it was. An empty path above all: to Diff a blank
// line means delete, and a rename must never trash a file.
func TestRenameRefuses(t *testing.T) {
	for _, tt := range []struct{ path, why string }{
		{"", "empty"},
		{"  ", "empty"},
		{"../out.go", ""},        // out of fndr's folder
		{"src/main.go", "exist"}, // taken: Apply never replaces
	} {
		m, root := renameModel(t)
		retype(m, tt.path)
		if !exists(filepath.Join(root, "src", "util.go")) {
			t.Fatalf("%q: util.go is gone", tt.path)
		}
		if m.rename == nil || m.rename.applying || !m.noticeErr || !strings.Contains(m.notice, tt.why) {
			t.Errorf("%q: rename on %v, notice %q; want it kept, with an error naming %q", tt.path, m.rename != nil, m.notice, tt.why)
		}
	}
}

// TestRenameOutsideTheFolder: found outside fndr's folder (GLOBAL, a
// frequent place), an entry is renamed by its whole path from ~: the name
// changed at the end renames it where it is; Enter on the path as it was
// does nothing.
func TestRenameOutsideTheFolder(t *testing.T) {
	m, root := browseModel(t, "src")
	m.home = filepath.Dir(root)
	readme := filepath.Join(root, "README.md")
	m.results[0].Entry = indexer.NewEntry(readme, 1, 0, false)
	key(m, 'r', tea.ModCtrl)
	want := "~/" + filepath.Base(root) + "/README.md"
	if m.rename == nil || m.rename.line.text != want {
		t.Fatalf("rename %+v; want the line %q", m.rename, want)
	}
	key(m, tea.KeyEnter, 0)
	if m.rename != nil || m.notice != "" || !exists(readme) {
		t.Fatalf("enter on the path as it was: rename on %v, notice %q", m.rename != nil, m.notice)
	}

	m.results[0].Entry = indexer.NewEntry(readme, 1, 0, false)
	key(m, 'r', tea.ModCtrl)
	for range len("README.md") {
		key(m, tea.KeyBackspace, 0)
	}
	typeText(m, "NOTES.md")
	key(m, tea.KeyEnter, 0)
	if !exists(filepath.Join(root, "NOTES.md")) || exists(readme) {
		t.Errorf("not renamed in place: notice %q", m.notice)
	}
}

// TestRenameEveryScreenSize: the line and status bar fit any screen.
func TestRenameEveryScreenSize(t *testing.T) {
	m, _ := renameModel(t)
	typeText(m, "/a/very/long/path/that/does/not/fit/anywhere/at/all.go")
	for w := 1; w <= 100; w++ {
		for h := 4; h <= 12; h++ {
			m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) != h {
				t.Fatalf("%dx%d: %d lines", w, h, len(lines))
			}
		}
	}
}

// TestRenameIntoNewFolderIsFound: a move into a folder that is not there
// yet makes it, and the index learns it at once: dirs shows it, not only
// after the next crawl. A folder that was there is not added twice.
func TestRenameIntoNewFolderIsFound(t *testing.T) {
	m, root := browseModel(t, "")
	typeText(m, "util")
	key(m, 'r', tea.ModCtrl)
	retype(m, "lib/x/util.go")
	for _, d := range []string{"lib", "lib/x"} {
		if !m.indexed(filepath.Join(root, d)) {
			t.Errorf("%s was made but is not indexed", d)
		}
	}

	key(m, 'u', tea.ModCtrl)
	typeText(m, "main")
	key(m, 'r', tea.ModCtrl)
	retype(m, "lib/main.go") // lib is there now
	if !exists(filepath.Join(root, "lib", "main.go")) {
		t.Fatalf("main.go not moved: notice %q", m.notice)
	}
	n := 0
	for _, e := range m.local.ix.Snapshot() {
		if e.Path == filepath.Join(root, "lib") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("lib is indexed %d times, want once", n)
	}
}
