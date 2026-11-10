package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// browseModel builds a real tree on disk, indexes it, and returns a model
// rooted at root/start with its first listing loaded:
//
//	README.md  docs/  src/main.go  src/util.go  .git/HEAD
func browseModel(t *testing.T, start string) (m *Model, root string) {
	t.Helper()
	root = t.TempDir()
	for _, f := range []string{"README.md", "src/main.go", "src/util.go", ".git/HEAD"} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, start)
	var ix indexer.Index
	out := make(chan []indexer.Entry)
	go func() {
		defer close(out)
		_, _ = indexer.Crawl(context.Background(), dir, nil, out)
	}()
	for batch := range out {
		ix.Add(batch)
	}

	m = newModel(dir, "", &ix)
	t.Cleanup(m.stop)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	send(m, m.Init()())
	return m, root
}

func (m *Model) names() []string {
	ns := make([]string, len(m.results))
	for i := range m.results {
		ns[i] = m.results[i].Entry.Name()
	}
	return ns
}

func (m *Model) selectName(t *testing.T, name string) {
	t.Helper()
	i := slices.Index(m.names(), name)
	if i < 0 {
		t.Fatalf("%q not in list %q", name, m.names())
	}
	m.cursor = i
}

func TestBrowseShowsFolder(t *testing.T) {
	m, _ := browseModel(t, "")
	want := []string{"docs", "src", "README.md", ".git"}
	if got := m.names(); !slices.Equal(got, want) {
		t.Errorf("listing = %q, want %q", got, want)
	}
}

func TestBrowseInAndBackOut(t *testing.T) {
	m, root := browseModel(t, "")

	m.selectName(t, "src")
	key(m, tea.KeyEnter, 0)
	if m.cwd != filepath.Join(root, "src") {
		t.Fatalf("cwd = %q after enter on src", m.cwd)
	}
	if got := m.names(); !slices.Equal(got, []string{"main.go", "util.go"}) {
		t.Errorf("listing of src = %q", got)
	}
	if status := ansi.Strip(m.viewStatus()); !strings.Contains(status, "src") {
		t.Errorf("status %q does not show the folder", status)
	}

	// Backspace on an empty query goes back out, landing on the folder.
	key(m, tea.KeyBackspace, 0)
	if m.cwd != root {
		t.Fatalf("cwd = %q after backspace, want %q", m.cwd, root)
	}
	if got := m.names()[m.cursor]; got != "src" {
		t.Errorf("selected %q after coming back, want src", got)
	}

	// "-" also goes up, as in Oil, but types itself inside a query.
	m.selectName(t, "src")
	key(m, tea.KeyEnter, 0)
	typeText(m, "-")
	if m.cwd != root {
		t.Errorf("cwd = %q after -, want %q", m.cwd, root)
	}
	typeText(m, "a-b")
	if m.query != "a-b" {
		t.Errorf("query = %q, want a-b", m.query)
	}
	key(m, 'u', tea.ModCtrl)

	// The skipped .git can still be entered by hand.
	m.selectName(t, ".git")
	key(m, tea.KeyEnter, 0)
	if got := m.names(); !slices.Equal(got, []string{"HEAD"}) {
		t.Errorf("listing of .git = %q", got)
	}
}

func TestBrowseEmptyFolderSaysSo(t *testing.T) {
	m, _ := browseModel(t, "docs")
	if lines := ansi.Strip(m.View().Content); !strings.Contains(lines, "empty folder") {
		t.Errorf("view does not say the folder is empty:\n%s", lines)
	}
}

// TestSearchFollowsFolder: inside a folder, typing searches that folder.
func TestSearchFollowsFolder(t *testing.T) {
	m, root := browseModel(t, "")
	m.selectName(t, "src")
	key(m, tea.KeyEnter, 0)

	typeText(m, "main")
	if got := m.resultPaths(); !slices.Equal(got, []string{filepath.Join(root, "src", "main.go")}) {
		t.Errorf("results = %q", got)
	}
	key(m, 'u', tea.ModCtrl)
	typeText(m, "readme")
	if len(m.results) != 0 {
		t.Errorf("found %q outside the current folder", m.resultPaths())
	}

	// Clearing the query brings the listing back.
	key(m, tea.KeyEscape, 0)
	if got := m.names(); !slices.Equal(got, []string{"main.go", "util.go"}) {
		t.Errorf("after esc: %q, want the listing", got)
	}
}

// TestUpAboveRootRecrawls: above the indexed root nothing is indexed, so a
// new crawl starts there, and batches of the old crawl are ignored.
func TestUpAboveRootRecrawls(t *testing.T) {
	m, root := browseModel(t, "src")
	oldGen := m.crawlGen

	key(m, tea.KeyBackspace, 0)
	if m.root != root || m.cwd != root {
		t.Fatalf("root=%q cwd=%q, want both %q", m.root, m.cwd, root)
	}
	if m.crawling || m.ix.Len() != 5 { // README.md docs src src/main.go src/util.go; .git is skipped
		t.Errorf("crawling=%v indexed=%d, want the new crawl finished with 5 entries", m.crawling, m.ix.Len())
	}

	send(m, batchMsg{gen: oldGen, entries: []indexer.Entry{indexer.NewEntry("/stale", 0, 0, false)}})
	if m.ix.Len() != 5 {
		t.Errorf("a batch from the old crawl changed the index: %d entries", m.ix.Len())
	}
}

func TestActivateFileOpens(t *testing.T) {
	m, _ := browseModel(t, "")
	m.selectName(t, "README.md")
	if cmd := m.activate(); cmd == nil {
		t.Error("enter on a file returned no command")
	}
	if m.cwd != m.root {
		t.Error("enter on a file changed the folder")
	}
}

// TestEnterLinkedFolder: Enter on a link to a folder goes to the folder it
// points to, where search works, instead of failing to open it as a file.
func TestEnterLinkedFolder(t *testing.T) {
	m, root := browseModel(t, "")
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "inside.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	send(m, m.load(m.cwd)())
	m.selectName(t, "link")
	send(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if m.cwd != want {
		t.Errorf("cwd %s, want the real folder %s", m.cwd, want)
	}
	if got := m.names(); !slices.Equal(got, []string{"inside.txt"}) {
		t.Errorf("listing %q", got)
	}
	if m.noticeErr {
		t.Errorf("error: %s", m.notice)
	}
}

// run carries out cmd, a batch or not, and every message it leads to.
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	case nil:
	default:
		send(m, msg)
	}
}

// TestIntoPrunedFolderRecrawls: the crawl lists a hidden folder without
// going in, so going into it must index it, or search there finds nothing
// while the listing shows files.
func TestIntoPrunedFolderRecrawls(t *testing.T) {
	m, root := browseModel(t, "")
	ci := filepath.Join(root, ".github", "workflows", "ci.yml")
	if err := os.MkdirAll(filepath.Dir(ci), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ci, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m.ix = &indexer.Index{} // index the tree again, the way fndr does: pruned
	run(m, m.startCrawl())
	if !m.indexed(filepath.Join(root, ".github")) || m.indexed(ci) {
		t.Fatal("want .github listed and not entered: nothing to test")
	}

	run(m, m.chdir(filepath.Join(root, ".github"), ""))
	typeText(m, "ci")
	if len(m.results) == 0 || m.results[0].Entry.Path != ci {
		t.Errorf("search in .github: %q, want ci.yml found", m.resultPaths())
	}
}
