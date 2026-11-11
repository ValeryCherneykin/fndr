package tui

import (
	"context"
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
