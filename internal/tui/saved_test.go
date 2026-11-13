package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// savedModel is a model as New makes it, started in src of the browse tree,
// with that tree as the home, and a saved index in a temporary cache that
// lists, besides the tree, src/gone.go: deleted since it was saved.
func savedModel(t *testing.T) (m *Model, home string) {
	t.Helper()
	m, home = browseModel(t, "src")
	m.home, m.prune = home, indexer.DefaultPrune(home)
	m.local.ix, m.local.crawling = &indexer.Index{}, true // nothing crawled yet: Init starts it
	m.savedPath = filepath.Join(t.TempDir(), "index")

	j := func(p string) string { return filepath.Join(home, p) }
	err := indexer.Save(m.savedPath, indexer.Saved{Root: home, Rules: m.prune.Rules(), Entries: []indexer.Entry{
		indexer.NewEntry(j("README.md"), 1, 1, false),
		indexer.NewEntry(j("src"), 1, 1, true),
		indexer.NewEntry(j("src/gone.go"), 1, 1, false),
		indexer.NewEntry(j("src/main.go"), 1, 1, false),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return m, home
}

// loaded hands m its saved index, as the start does, and returns the
// crawls that follow, not run.
func loaded(t *testing.T, m *Model) tea.Cmd {
	t.Helper()
	msg := readSaved(t, m)
	return m.savedLoaded(&msg)
}

func readSaved(t *testing.T, m *Model) savedMsg {
	t.Helper()
	msg, ok := loadSaved(m.savedPath)().(savedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("reading the saved index: %+v", msg.err)
	}
	return msg
}

func count(ix *indexer.Index, path string) int {
	n := 0
	for _, e := range ix.Snapshot() {
		if e.Path == path {
			n++
		}
	}
	return n
}

// TestStartFromSaved: search works on the saved index before any crawl,
// LOCAL on its part below the folder, without "indexing"; the crawls
// then replace it with what is on the disk, and the home's is saved.
func TestStartFromSaved(t *testing.T) {
	m, home := savedModel(t)
	crawls := loaded(t, m)

	gone := filepath.Join(home, "src", "gone.go")
	if m.local.ix.Len() != 2 || count(m.local.ix, gone) != 1 {
		t.Fatalf("LOCAL from the saved index: %d entries, want src's 2", m.local.ix.Len())
	}
	typeText(m, "gone")
	if got := m.resultPaths(); !slices.Equal(got, []string{gone}) {
		t.Errorf("search before any crawl: %q, want the saved gone.go", got)
	}
	if strings.Contains(m.status(), "indexing") {
		t.Errorf("status %q: a refresh is quiet, what is searched is whole", m.status())
	}

	run(m, crawls)
	if got := m.resultPaths(); len(got) != 0 {
		t.Errorf("after the crawls: %q, want gone.go gone", got)
	}
	readme, util := filepath.Join(home, "README.md"), filepath.Join(home, "src", "util.go")
	if count(m.global.ix, readme) != 1 || count(m.global.ix, gone) != 0 || count(m.local.ix, util) != 1 {
		t.Errorf("after the crawls: README.md %d, gone.go %d in GLOBAL, util.go %d in LOCAL; want 1, 0, 1",
			count(m.global.ix, readme), count(m.global.ix, gone), count(m.local.ix, util))
	}
	saved, err := indexer.Load(m.savedPath)
	if err != nil {
		t.Fatal(err)
	}
	if paths := entryPaths(saved.Entries); slices.Contains(paths, gone) || !slices.Contains(paths, util) {
		t.Errorf("saved after the crawl: %q", paths)
	}
}

func entryPaths(es []indexer.Entry) []string {
	ps := make([]string, len(es))
	for i := range es {
		ps[i] = es[i].Path
	}
	return ps
}

// TestSavedUnderOtherRules: an index crawled with other folders left out
// may lack what is searched now: not used, crawled as if there were none.
func TestSavedUnderOtherRules(t *testing.T) {
	m, _ := savedModel(t)
	msg := readSaved(t, m)
	msg.saved.Rules = "other"
	m.savedLoaded(&msg)
	if m.global.ix.Len() != 0 || m.global.next != nil || !strings.Contains(m.status(), "indexing") {
		t.Errorf("GLOBAL %d entries, refreshing %v, status %q: want a plain crawl",
			m.global.ix.Len(), m.global.next != nil, m.status())
	}
}

// TestRenameDuringRefresh: a rename while the crawl refreshes survives the
// swap, whether the crawl read the folder before the rename or after: the
// new name once, the old one gone.
func TestRenameDuringRefresh(t *testing.T) {
	for _, crawlSaw := range []string{"before", "after"} {
		t.Run(crawlSaw, func(t *testing.T) {
			m, home := savedModel(t)
			loaded(t, m)
			from, to := filepath.Join(home, "src", "main.go"), filepath.Join(home, "src", "app.go")
			m.updateIndexes([]string{from}, []indexer.Entry{indexer.NewEntry(to, 1, 1, false)})

			seen := from
			if crawlSaw == "after" {
				seen = to
			}
			s := m.global
			s.next.Add([]indexer.Entry{indexer.NewEntry(seen, 1, 1, false)})
			m.crawlDone(&crawlDoneMsg{gen: s.gen})
			if count(s.ix, to) != 1 || count(s.ix, from) != 0 {
				t.Errorf("after the swap: app.go %d, main.go %d; want 1, 0", count(s.ix, to), count(s.ix, from))
			}
		})
	}
}

// TestRefreshCutShort: a crawl that did not finish replaces nothing, and
// saves nothing: a part of the home is not the home.
func TestRefreshCutShort(t *testing.T) {
	m, home := savedModel(t)
	loaded(t, m)
	s, before := m.global, m.global.ix.Len()
	s.next.Add([]indexer.Entry{indexer.NewEntry(filepath.Join(home, "README.md"), 1, 1, false)})
	if cmd := m.crawlDone(&crawlDoneMsg{gen: s.gen, err: context.Canceled}); cmd != nil {
		run(m, cmd)
	}
	if s.ix.Len() != before || s.next != nil {
		t.Errorf("index %d entries (was %d), refresh left %v", s.ix.Len(), before, s.next != nil)
	}
	saved, err := indexer.Load(m.savedPath)
	if err != nil || len(saved.Entries) != 4 {
		t.Errorf("the saved index changed: %d entries, %v", len(saved.Entries), err)
	}
}

// TestCloseFinishesTheSave: quitting right after a crawl must not cut the
// save of the index off: Close writes one whose command never ran (and
// waits for one running), so no temporary file is left with every file
// name of the home in it, and the next start is instant.
func TestCloseFinishesTheSave(t *testing.T) {
	m, home := savedModel(t)
	if err := os.Remove(m.savedPath); err != nil {
		t.Fatal(err)
	}
	m.global = &scope{root: home, ix: &indexer.Index{}}
	m.global.ix.Add([]indexer.Entry{indexer.NewEntry(filepath.Join(home, "README.md"), 1, 1, false)})
	_ = m.saveIndex() // handed out, never run: the program quit first

	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if s, err := indexer.Load(m.savedPath); err != nil || len(s.Entries) != 1 {
		t.Errorf("after Close: %d entries, %v; want the index saved", len(s.Entries), err)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(m.savedPath), ".index-*")) //nolint:errcheck // valid pattern
	if len(left) != 0 {
		t.Errorf("left behind: %q", left)
	}
}

// TestChdirTakesFromGlobal: going where LOCAL has no index (up, or Enter
// on a folder found in GLOBAL) searches GLOBAL's entries below it at once,
// without "indexing", and the crawl that follows corrects them quietly.
// Not while GLOBAL's first crawl runs: a part of the home is not a folder.
func TestChdirTakesFromGlobal(t *testing.T) {
	m, home := savedModel(t)
	crawls := loaded(t, m)
	gone := filepath.Join(home, "src", "gone.go")

	cmd := m.up()
	if m.local.root != home || count(m.local.ix, gone) != 1 || m.local.next == nil {
		t.Fatalf("after up: root %q, gone.go %d, refreshing %v; want the home from GLOBAL",
			m.local.root, count(m.local.ix, gone), m.local.next != nil)
	}
	if strings.Contains(m.status(), "indexing") {
		t.Errorf("status %q: what is searched is whole", m.status())
	}
	run(m, tea.Batch(crawls, cmd))
	if count(m.local.ix, gone) != 0 || count(m.local.ix, filepath.Join(home, "src", "util.go")) != 1 {
		t.Errorf("after the crawl: LOCAL is not what is on the disk")
	}

	t.Run("not the folder itself", func(t *testing.T) {
		m, home := savedModel(t)
		loaded(t, m)
		src := filepath.Join(home, "src")
		m.local.root = filepath.Join(home, "elsewhere") // as if started there
		m.chdir(src, "")
		if count(m.local.ix, src) != 0 || count(m.local.ix, filepath.Join(src, "main.go")) != 1 {
			t.Errorf("LOCAL in src: src %d, main.go %d; want 0, 1: a crawl never lists its root",
				count(m.local.ix, src), count(m.local.ix, filepath.Join(src, "main.go")))
		}
	})

	t.Run("first crawl", func(t *testing.T) {
		// A folder of the home entered outside LOCAL while GLOBAL's first
		// crawl is under way: what GLOBAL holds of it is a part, not taken.
		m, home := savedModel(t)
		m.global = &scope{root: home, ix: &indexer.Index{}}
		m.startCrawl(m.global) // never run: a crawl under way, nothing whole yet
		src := filepath.Join(home, "src")
		m.global.ix.Add([]indexer.Entry{indexer.NewEntry(filepath.Join(src, "main.go"), 1, 1, false)})
		m.local.root = filepath.Join(home, "elsewhere") // as if started there
		m.chdir(src, "")
		if m.local.ix.Len() != 0 || m.local.next != nil {
			t.Errorf("LOCAL took %d entries from a crawl under way", m.local.ix.Len())
		}
	})
}

// TestHomeIsOneScope: started in the home, LOCAL and GLOBAL are one index,
// not the same paths held twice; a folder left for elsewhere gets its own,
// GLOBAL staying the home; coming back home shares it again.
func TestHomeIsOneScope(t *testing.T) {
	m, home := savedModel(t)
	m.local.root, m.cwd = home, home // started in the home
	loaded(t, m)
	if m.local != m.global {
		t.Fatal("started in the home: LOCAL and GLOBAL are two indexes")
	}
	elsewhere := t.TempDir()
	m.chdir(elsewhere, "")
	if m.local == m.global || m.global.root != home || m.local.root != elsewhere {
		t.Fatalf("elsewhere: local %q, global %q, shared %v", m.local.root, m.global.root, m.local == m.global)
	}
	m.chdir(home, "")
	if m.local != m.global {
		t.Error("back home: LOCAL did not share GLOBAL's index")
	}
}

// TestOneScopeUpdatedOnce: an edit reaches a shared index once, or a
// renamed file would be found twice.
func TestOneScopeUpdatedOnce(t *testing.T) {
	m, home := savedModel(t)
	m.local.root, m.cwd = home, home
	loaded(t, m)
	p := filepath.Join(home, "new.txt")
	m.updateIndexes(nil, []indexer.Entry{indexer.NewEntry(p, 1, 1, false)})
	if n := count(m.local.ix, p); n != 1 {
		t.Errorf("new.txt is in the index %d times, want once", n)
	}
}

// TestChdirStopsLocalCrawl: a folder left for one outside LOCAL ends the
// old LOCAL's crawl, which no one would read; never the home's, if shared.
func TestChdirStopsLocalCrawl(t *testing.T) {
	m, _ := savedModel(t)
	loaded(t, m) // LOCAL is src, GLOBAL the home: two scopes, two crawls
	stopped := false
	m.local.stop = func() { stopped = true }
	m.chdir(t.TempDir(), "")
	if !stopped {
		t.Error("the old LOCAL's crawl goes on")
	}

	m, home := savedModel(t)
	m.local.root, m.cwd = home, home
	loaded(t, m) // one scope
	stopped = false
	m.global.stop = func() { stopped = true }
	m.chdir(t.TempDir(), "")
	if stopped {
		t.Error("leaving the home stopped GLOBAL's crawl")
	}
}

// TestTabSharesHome: with no saved index, Tab in the home searches the
// index LOCAL is already crawling, not a second crawl of the same home.
func TestTabSharesHome(t *testing.T) {
	m, home := browseModel(t, "")
	m.home = home
	key(m, tea.KeyTab, 0)
	if m.global != m.local {
		t.Error("Tab in the home started a second index of it")
	}
}

// TestReleaseAfterHomeOnly: memory goes back to the system after the
// home's crawl, the big one, and after its save, not beside it; not after
// every folder entered.
func TestReleaseAfterHomeOnly(t *testing.T) {
	m, _ := savedModel(t)
	loaded(t, m)        // LOCAL is src, GLOBAL the home
	var released []bool // whether the index was saved by then, per release
	m.release = func() { _, err := os.Stat(m.savedPath); released = append(released, err == nil) }
	os.Remove(m.savedPath) //nolint:errcheck,gosec // gone, so a save can be seen
	run(m, m.crawlDone(&crawlDoneMsg{gen: m.local.gen}))
	if len(released) != 0 {
		t.Fatal("released after a folder's crawl")
	}
	run(m, m.crawlDone(&crawlDoneMsg{gen: m.global.gen}))
	if !slices.Equal(released, []bool{true}) {
		t.Errorf("releases %v: want one, after the index was saved", released)
	}
}
