package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// TestOilCopy: yy, p, the name changed, :w shows the copy, y makes it;
// the original stays, the copy is found at once, the status says so.
func TestOilCopy(t *testing.T) {
	m, root, _ := writeModel(t)
	goTo(t, m, "README.md")
	typeText(m, "yyp")
	typeText(m, "ccNOTES.md")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	if v := m.View().Content; !strings.Contains(v, "copy") || !strings.Contains(v, "README.md → NOTES.md") {
		t.Fatalf("plan lacks the copy:\n%s", v)
	}
	typeText(m, "y")
	if content := func(p string) string { b, _ := os.ReadFile(p); return string(b) }; content(filepath.Join(root, "NOTES.md")) != "x" || !exists(filepath.Join(root, "README.md")) {
		t.Fatalf("copy not made, or the original gone: notice %q", m.notice)
	}
	if m.notice != "copied 1" || !m.indexed(filepath.Join(root, "NOTES.md")) {
		t.Errorf("notice %q, indexed %v", m.notice, m.indexed(filepath.Join(root, "NOTES.md")))
	}
}

// TestOilCopyFolderCounted: a folder's copy says in the plan how much it
// holds, as a folder trashed does.
func TestOilCopyFolderCounted(t *testing.T) {
	m, _, _ := writeModel(t)
	goTo(t, m, "src/")
	typeText(m, "yyp")
	typeText(m, "cclib/")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	if v := m.View().Content; !strings.Contains(v, "src/ → lib/ (2 items)") {
		t.Errorf("plan:\n%s", v)
	}
}

// TestOilResultsCopyAway: what a search found is copied anywhere by a
// whole path.
func TestOilResultsCopyAway(t *testing.T) {
	m, root, home := awayModel(t)
	m.global = &scope{root: home, ix: &indexer.Index{}} // the home's index, where ~/backup is
	tag(m, ".go")
	key(m, 'o', tea.ModCtrl)
	goTo(t, m, "src/main.go")
	typeText(m, "yyp")
	typeText(m, "cc~/backup/main.go")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")
	if !exists(filepath.Join(home, "backup", "main.go")) || !exists(filepath.Join(root, "src", "main.go")) {
		t.Errorf("copy away not made, or the original gone: notice %q", m.notice)
	}
	found := false
	for _, e := range m.global.ix.Snapshot() {
		found = found || e.Path == filepath.Join(home, "backup")
	}
	if !found {
		t.Error("~/backup was made by the copy but the home's index does not know it")
	}
}
