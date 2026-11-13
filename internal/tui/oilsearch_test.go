package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
	"github.com/ValeryCherneykin/fndr/internal/oil"
)

// resultsModel is browseModel (README.md, src/main.go, src/util.go,
// docs/) in a home of its own, with the tag .go on and OIL entered on what
// it found.
func resultsModel(t *testing.T) (m *Model, root string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // the trash goes in it
	t.Setenv("XDG_DATA_HOME", "")
	m, root = browseModel(t, "")
	tag(m, ".go")
	key(m, 'o', tea.ModCtrl)
	if m.oil == nil || m.oil.search == nil {
		t.Fatalf("ctrl+o with a tag on: oil %+v, notice %q; want a buffer of results", m.oil, m.notice)
	}
	return m, root
}

// goTo puts the cursor on the line that reads text.
func goTo(t *testing.T, m *Model, text string) {
	t.Helper()
	i := slices.Index(m.oilTexts(), text)
	if i < 0 {
		t.Fatalf("no line %q in %q", text, m.oilTexts())
	}
	m.oil.buf.GoTo(i)
}

// TestOilResultsShows: every entry found is a line, its path from the
// folder searched; the header says what found them, and how many.
func TestOilResultsShows(t *testing.T) {
	m, _ := resultsModel(t)
	if got := sorted(m.oilTexts()); !slices.Equal(got, []string{"src/main.go", "src/util.go"}) {
		t.Errorf("lines %q, want the paths of both .go files", got)
	}
	if head := strings.SplitN(m.View().Content, "\n", 2)[0]; !strings.Contains(head, ".go") || !strings.Contains(head, "2 found") {
		t.Errorf("header %q", head)
	}
}

// TestOilResultsRename: a line renamed, and moved by its folders, is done
// by :w and y; after y the buffer is what was written, even what the
// search would no longer find (util.txt is not .go).
func TestOilResultsRename(t *testing.T) {
	m, root := resultsModel(t)
	goTo(t, m, "src/util.go")
	typeText(m, "ccsrc/util.txt")
	key(m, tea.KeyEscape, 0)
	goTo(t, m, "src/main.go")
	typeText(m, "ccdocs/main.go")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")

	if !exists(filepath.Join(root, "src", "util.txt")) || !exists(filepath.Join(root, "docs", "main.go")) {
		t.Fatalf("not renamed and moved: notice %q", m.notice)
	}
	if m.oil == nil || m.oil.search == nil {
		t.Fatal("after y: want the buffer of results still")
	}
	if got := sorted(m.oilTexts()); !slices.Equal(got, []string{"docs/main.go", "src/util.txt"}) {
		t.Errorf("after y: %q, want what was written", got)
	}
	if !m.indexed(filepath.Join(root, "docs", "main.go")) || m.indexed(filepath.Join(root, "src", "main.go")) {
		t.Error("the index did not follow the move")
	}
}

// TestOilResultsTrash: dd sends what was found to the trash; leaving, the
// search is back, its tag on, finding what is left.
func TestOilResultsTrash(t *testing.T) {
	m, root := resultsModel(t)
	goTo(t, m, "src/util.go")
	typeText(m, "dd")
	command(m, "w")
	typeText(m, "y")
	if exists(filepath.Join(root, "src", "util.go")) {
		t.Fatalf("util.go is still there: notice %q", m.notice)
	}
	if got := m.oilTexts(); !slices.Equal(got, []string{"src/main.go"}) {
		t.Errorf("after y: %q", got)
	}
	key(m, tea.KeyEscape, 0)
	if m.oil != nil || !slices.Equal(m.tagTexts(), []string{".go"}) {
		t.Fatalf("esc: oil %v, tags %q; want the search, tag kept", m.oil != nil, m.tagTexts())
	}
	if got := m.names(); !slices.Equal(got, []string{"main.go"}) {
		t.Errorf("the search after: %q", got)
	}
}

// TestOilResultsCreate: o makes a path from the folder searched.
func TestOilResultsCreate(t *testing.T) {
	m, root := resultsModel(t)
	typeText(m, "onew/x.go")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")
	if !exists(filepath.Join(root, "new", "x.go")) {
		t.Fatalf("not created: notice %q", m.notice)
	}
	if !slices.Contains(m.oilTexts(), "new/x.go") {
		t.Errorf("after y: %q, want the new line kept", m.oilTexts())
	}
}

// TestOilResultsWalk: - goes up to the folder searched, Enter on a
// folder into it: both as a folder, no longer results.
func TestOilResultsWalk(t *testing.T) {
	m, root := resultsModel(t)
	typeText(m, "-")
	if m.oil == nil || m.oil.search != nil || m.oil.dir != root {
		t.Fatalf("-: %+v, want the folder searched", m.oil)
	}

	m, root = browseModel(t, "")
	tag(m, "dirs")
	key(m, 'o', tea.ModCtrl)
	goTo(t, m, "src/")
	key(m, tea.KeyEnter, 0)
	if m.oil == nil || m.oil.search != nil || m.oil.dir != filepath.Join(root, "src") {
		t.Fatalf("enter on src/: %+v, want src as a folder", m.oil)
	}
}

// TestOilResultsTooMany: past oilMaxResults, the search is narrowed first:
// ggdG must never reach a whole home.
func TestOilResultsTooMany(t *testing.T) {
	paths := make([]string, oilMaxResults+1)
	for i := range paths {
		paths[i] = fmt.Sprintf("/r/d%d/f.go", i)
	}
	m := testModel(t, paths...)
	tag(m, ".go")
	key(m, 'o', tea.ModCtrl)
	if m.oil != nil || !m.noticeErr || !strings.Contains(m.notice, "narrow the search") {
		t.Errorf("oil %v, notice %q; want a refusal", m.oil != nil, m.notice)
	}
}

// TestOilResultsWhileIndexing: a buffer of a first crawl's part is not
// what was found; ctrl+o waits for the crawl.
func TestOilResultsWhileIndexing(t *testing.T) {
	m := tagModel(t)
	m.local.crawling = true
	tag(m, ".go")
	key(m, 'o', tea.ModCtrl)
	if m.oil != nil || !strings.Contains(m.notice, "still indexing") {
		t.Errorf("oil %v, notice %q", m.oil != nil, m.notice)
	}
}

// TestOilResultsNested: a folder and what is inside it both changed are
// refused before any plan, the cursor on the line to look at.
func TestOilResultsNested(t *testing.T) {
	m, _ := browseModel(t, "")
	typeText(m, "src")
	key(m, 'o', tea.ModCtrl)
	if !slices.Contains(m.oilTexts(), "src/") || !slices.Contains(m.oilTexts(), "src/util.go") {
		t.Fatalf("lines %q", m.oilTexts())
	}
	goTo(t, m, "src/")
	typeText(m, "cclib/")
	key(m, tea.KeyEscape, 0)
	goTo(t, m, "src/util.go")
	typeText(m, "ccsrc/u.go")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	if m.oil.plan != nil || !strings.Contains(m.notice, "inside a folder that changes too") || m.oilTexts()[m.oil.buf.Row()] != "src/u.go" {
		t.Errorf("plan %v, notice %q, cursor on %q", m.oil.plan, m.notice, m.oilTexts()[m.oil.buf.Row()])
	}
}

// TestOilResultsKeys: ctrl+r reads the results again, not the folder; g.
// is a folder's; after y the cursor stays on its line.
func TestOilResultsKeys(t *testing.T) {
	m, root := resultsModel(t)
	writeFile(t, filepath.Join(root, "src", "late.go")) // after the search
	key(m, 'r', tea.ModCtrl)
	if m.oil.search == nil || slices.Contains(m.oilTexts(), "src/late.go") || len(m.oilTexts()) != 2 {
		t.Errorf("ctrl+r: %q, want the same two results read again", m.oilTexts())
	}
	typeText(m, "g.")
	if m.oil.search == nil || !strings.Contains(m.notice, "g. is for a folder") {
		t.Errorf("g.: notice %q", m.notice)
	}

	row := 1 // not the first line: the cursor starts there anyway
	m.oil.buf.GoTo(row)
	typeText(m, "ccsrc/u.go")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")
	if m.oil.buf.Row() != row || m.oilTexts()[row] != "src/u.go" {
		t.Errorf("after y: cursor on line %d %q, want %d src/u.go", m.oil.buf.Row(), m.oilTexts()[m.oil.buf.Row()], row)
	}
}

// TestOilResultsHidden: what a search found is shown, hidden or not.
func TestOilResultsHidden(t *testing.T) {
	m, root := browseModel(t, "")
	writeFile(t, filepath.Join(root, "src", ".env"))
	send(m, m.startCrawl(m.local)()) // index it
	typeText(m, ".env")
	key(m, 'o', tea.ModCtrl)
	if m.oil == nil || !slices.Contains(m.oilTexts(), "src/.env") {
		t.Errorf("lines %q, want src/.env", m.oilTexts())
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestOilResultsMoveIntoNewFolder: as with ctrl+r, a folder made on the
// way is indexed at once.
func TestOilResultsMoveIntoNewFolder(t *testing.T) {
	m, root := resultsModel(t)
	goTo(t, m, "src/util.go")
	typeText(m, "ccpkg/util.go")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")
	if !exists(filepath.Join(root, "pkg", "util.go")) || !m.indexed(filepath.Join(root, "pkg")) {
		t.Errorf("pkg made: %v, indexed: %v", exists(filepath.Join(root, "pkg")), m.indexed(filepath.Join(root, "pkg")))
	}
}

// TestOilResultsHalfWritten: a plan that stops halfway leaves the buffer
// true to the disk: the line trashed is gone, the move that failed shows
// where the file still is, not the path that was written.
func TestOilResultsHalfWritten(t *testing.T) {
	m, root := resultsModel(t)
	ro := filepath.Join(root, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil { //nolint:gosec // read-only on purpose
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) }) //nolint:gosec,errcheck // so TempDir can remove it
	goTo(t, m, "src/util.go")
	typeText(m, "dd")
	goTo(t, m, "src/main.go")
	typeText(m, "ccro/main.go")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")

	if !m.noticeErr || !strings.Contains(m.notice, "1 of 2 done") {
		t.Fatalf("notice %q, want 1 of 2 done", m.notice)
	}
	if exists(filepath.Join(root, "src", "util.go")) || !exists(filepath.Join(root, "src", "main.go")) {
		t.Fatal("the disk is not what the test means: util.go trashed, main.go not moved")
	}
	if got := m.oilTexts(); !slices.Equal(got, []string{"src/main.go"}) {
		t.Errorf("buffer %q, want only main.go, where it still is", got)
	}
	if m.oil.changes != 0 {
		t.Errorf("%d changes: the buffer is the disk now, nothing to write", m.oil.changes)
	}
}

// TestOilRereadKeepsWhatStayed: read again, a buffer shows an entry whose
// line was deleted if it is still on the disk (its trash failed), and not
// one that is gone.
func TestOilRereadKeepsWhatStayed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stayed.go"))
	st := &oilState{
		dir:    root,
		search: &oilSearch{},
		items:  []mutation.Item{{ID: 1, Name: "stayed.go"}, {ID: 2, Name: "gone.go"}},
		buf:    oil.New([]mutation.Line{{ID: 0, Text: ""}}),
	}
	msg, ok := st.oilReread()().(oilMsg)
	if !ok {
		t.Fatal("not an oilMsg")
	}
	if len(msg.entries) != 1 || msg.entries[0].Path != filepath.Join(root, "stayed.go") {
		t.Errorf("entries %v, want stayed.go alone", msg.entries)
	}
}
