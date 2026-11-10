package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// globalModel is browseModel started in src, with the tree's root as the
// home GLOBAL searches.
func globalModel(t *testing.T) (m *Model, home string) {
	t.Helper()
	m, home = browseModel(t, "src")
	m.home = home
	return m, home
}

func (m *Model) status() string {
	lines := strings.Split(m.View().Content, "\n")
	return ansi.Strip(lines[len(lines)-1])
}

// TestTabSearchesHome: LOCAL finds only what is below the folder; Tab
// crawls the home and searches all of it, with the same query; Tab again
// is LOCAL as it was.
func TestTabSearchesHome(t *testing.T) {
	m, home := globalModel(t)
	typeText(m, "readme")
	if len(m.results) != 0 {
		t.Fatalf("LOCAL in src found %q", m.resultPaths())
	}
	if m.global != nil {
		t.Fatal("the home was crawled before Tab")
	}

	key(m, tea.KeyTab, 0)
	readme := filepath.Join(home, "README.md")
	if len(m.results) == 0 || m.results[0].Entry.Path != readme {
		t.Errorf("GLOBAL: %q, want README.md", m.resultPaths())
	}
	if m.query != "readme" || !strings.HasPrefix(strings.TrimSpace(m.status()), "GLOBAL  ~ ") {
		t.Errorf("query %q, status %q; want the query kept and GLOBAL shown", m.query, m.status())
	}
	if m.local.ix.Len() != 2 {
		t.Errorf("the local index holds %d entries: the home crawl leaked into it", m.local.ix.Len())
	}

	key(m, tea.KeyTab, 0)
	if len(m.results) != 0 || m.query != "readme" || !strings.Contains(m.status(), "LOCAL") {
		t.Errorf("back in LOCAL: %q, query %q, status %q", m.resultPaths(), m.query, m.status())
	}
}

// TestGlobalEsc: esc steps back one thing at a time: the query, then
// GLOBAL, then fndr.
func TestGlobalEsc(t *testing.T) {
	m, _ := globalModel(t)
	key(m, tea.KeyTab, 0)
	typeText(m, "x")
	if key(m, tea.KeyEscape, 0) || m.query != "" || !m.globalOn {
		t.Fatalf("first esc: query %q, global %v; want the query cleared only", m.query, m.globalOn)
	}
	if key(m, tea.KeyEscape, 0) || m.globalOn {
		t.Fatal("second esc: want back to LOCAL, not out")
	}
	if !key(m, tea.KeyEscape, 0) {
		t.Error("third esc: want fndr to quit")
	}
}

// TestGlobalEnterGoesLocal: a folder found in GLOBAL is entered, and the
// search is LOCAL there, as with any folder entered.
func TestGlobalEnterGoesLocal(t *testing.T) {
	m, home := globalModel(t)
	key(m, tea.KeyTab, 0)
	typeText(m, "docs")
	m.selectName(t, "docs")
	key(m, tea.KeyEnter, 0)
	if m.cwd != filepath.Join(home, "docs") || m.globalOn {
		t.Errorf("cwd %q, global %v; want LOCAL in docs", m.cwd, m.globalOn)
	}
}

// TestGlobalWithoutHome: with no home to search, Tab says so.
func TestGlobalWithoutHome(t *testing.T) {
	m, _ := browseModel(t, "")
	key(m, tea.KeyTab, 0)
	if m.globalOn || !m.noticeErr {
		t.Errorf("global %v, notice %q; want a refusal", m.globalOn, m.notice)
	}
}

// TestRenameUpdatesBothIndexes: a rename is seen by GLOBAL search too.
func TestRenameUpdatesBothIndexes(t *testing.T) {
	m, home := globalModel(t)
	key(m, tea.KeyTab, 0)
	key(m, tea.KeyTab, 0) // the home is crawled; back to LOCAL, in src
	typeText(m, "util")
	key(m, 'r', tea.ModCtrl)
	retype(m, "helpers.go")
	moved := filepath.Join(home, "src", "helpers.go")
	if !indexed(m.global.ix, moved) {
		t.Errorf("the GLOBAL index does not know %s", moved)
	}
}

func indexed(ix *indexer.Index, path string) bool {
	for _, e := range ix.Snapshot() {
		if e.Path == path {
			return true
		}
	}
	return false
}
