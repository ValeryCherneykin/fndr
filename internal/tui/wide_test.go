package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// wideModel is a project whose build/ is in .gitignore and which has a
// node_modules, crawled under the usual rules: neither's contents found.
func wideModel(t *testing.T) (m *Model, root string) {
	t.Helper()
	root = t.TempDir()
	for f, body := range map[string]string{
		".gitignore": "build/\n", "build/app.bin": "x", "node_modules/pkg/index.js": "x",
		"src/main.go": "x", ".venv/lib/x.py": "x",
	} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m = newModel(root, "", &indexer.Index{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	run(m, m.startCrawl(m.local))
	return m, root
}

// TestWide: ctrl+d searches what .gitignore and node_modules leave out
// too, and says so; hidden folders stay out. Again, it is off.
func TestWide(t *testing.T) {
	m, root := wideModel(t)
	typeText(m, "app.bin")
	if len(m.results) != 0 {
		t.Fatalf("build/ searched under the usual rules: %q", m.resultPaths())
	}
	key(m, 'd', tea.ModCtrl)
	if got := m.resultPaths(); !slices.Equal(got, []string{filepath.Join(root, "build", "app.bin")}) {
		t.Errorf("with ignored: %q", got)
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "+ignored") {
		t.Errorf("the status does not say it:\n%s", v)
	}
	key(m, 'u', tea.ModCtrl)
	typeText(m, "index.js")
	if len(m.results) != 1 {
		t.Errorf("node_modules: %q", m.resultPaths())
	}
	key(m, 'u', tea.ModCtrl)
	typeText(m, "x.py")
	if len(m.results) != 0 {
		t.Errorf("a hidden folder searched: %q", m.resultPaths())
	}
	key(m, 'd', tea.ModCtrl)
	key(m, 'u', tea.ModCtrl)
	typeText(m, "app.bin")
	if m.wide != nil || len(m.results) != 0 || strings.Contains(ansi.Strip(m.View().Content), "+ignored") {
		t.Errorf("off again: wide %v, %q", m.wide != nil, m.resultPaths())
	}
}

// TestWideEndsWithTheScope: a folder entered, or Tab, ends it, and its
// crawl: it searched the scope left.
func TestWideEndsWithTheScope(t *testing.T) {
	m, root := wideModel(t)
	key(m, 'd', tea.ModCtrl)
	w := m.wide
	run(m, m.chdir(filepath.Join(root, "src"), ""))
	if m.wide != nil || slices.Contains(m.scopes(), w) || m.crawled(w.gen) != nil {
		t.Errorf("chdir: wide %v; its crawl still taken in %v", m.wide != nil, m.crawled(w.gen) != nil)
	}
	key(m, 'd', tea.ModCtrl)
	m.home = root
	key(m, tea.KeyTab, 0)
	if m.wide != nil {
		t.Error("Tab: still on")
	}
}
