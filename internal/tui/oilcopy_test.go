package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
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

// TestOilYankAcrossFolders: yy in one folder, Enter into another, p, :w,
// y copies it there; the original stays, and the plan names the whole path
// it comes from.
func TestOilYankAcrossFolders(t *testing.T) {
	m, root, _ := writeModel(t)
	goTo(t, m, "README.md")
	typeText(m, "yy")
	goTo(t, m, "docs/")
	key(m, tea.KeyEnter, 0) // OIL into docs
	if m.oil == nil || m.oil.dir != filepath.Join(root, "docs") {
		t.Fatal("Enter did not open docs in OIL")
	}
	typeText(m, "p")
	if got := m.oilTexts(); !slices.Contains(got, "README.md") {
		t.Fatalf("p put nothing: %q", got)
	}
	m.oil.home = root // the plan says where from as ~/…, short
	command(m, "w")
	if v := m.View().Content; !strings.Contains(v, "~/README.md → README.md") {
		t.Errorf("plan does not show where from:\n%s", v)
	}
	want := mutation.Op{Kind: mutation.OpCopy, From: filepath.Join(root, "README.md"), To: "README.md"}
	if !slices.Equal(m.oil.plan, mutation.Plan{want}) {
		t.Fatalf("plan %v, want %v", m.oil.plan, want)
	}
	typeText(m, "y")
	if !exists(filepath.Join(root, "docs", "README.md")) || !exists(filepath.Join(root, "README.md")) {
		t.Fatalf("copy not made, or the original gone: notice %q", m.notice)
	}
	if m.notice != "copied 1" || !m.indexed(filepath.Join(root, "docs", "README.md")) {
		t.Errorf("notice %q, indexed %v", m.notice, m.indexed(filepath.Join(root, "docs", "README.md")))
	}
}

// TestOilYankOutlivesOil: what was yanked waits while fndr is searching,
// and a folder copied in this way keeps what it holds. Not putting it
// changes nothing: an item yanked elsewhere is never this buffer's to trash.
func TestOilYankOutlivesOil(t *testing.T) {
	m, root, _ := writeModel(t)
	goTo(t, m, "src/")
	typeText(m, "yy")
	key(m, tea.KeyEscape, 0) // back to search
	if m.oil != nil {
		t.Fatal("esc did not leave OIL")
	}
	run(m, m.chdir(filepath.Join(root, "docs"), ""))
	key(m, 'o', tea.ModCtrl)
	if m.oil == nil || m.oil.changes != 0 {
		t.Fatalf("OIL in docs: %v, changes before p", m.oil)
	}
	typeText(m, "p")
	command(m, "w")
	from := filepath.Join(root, "src")
	want := mutation.Op{Kind: mutation.OpCopy, From: from, To: "src"}
	if !slices.Equal(m.oil.plan, mutation.Plan{want}) || m.oil.trashCounts[from] != 2 {
		t.Fatalf("plan %v, counts %v; want %v, 2 items", m.oil.plan, m.oil.trashCounts, want)
	}
	typeText(m, "y")
	for _, f := range []string{"docs/src/main.go", "docs/src/util.go", "src/main.go"} {
		if !exists(filepath.Join(root, f)) {
			t.Errorf("%s missing: notice %q", f, m.notice)
		}
	}
}

// TestOilYankSameFolderAgain: yanked, then the buffer read again (ctrl+r):
// the entry is the buffer's own again, so p is a copy within the folder,
// with no whole path.
func TestOilYankSameFolderAgain(t *testing.T) {
	m, _, _ := writeModel(t)
	goTo(t, m, "README.md")
	typeText(m, "yy")
	key(m, 'r', tea.ModCtrl)
	if len(m.oil.items) != m.oil.own {
		t.Fatalf("the folder's own entry came back as Away: %+v", m.oil.items)
	}
	typeText(m, "p")
	typeText(m, "ccA.md")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	if v := m.View().Content; !strings.Contains(v, "README.md → A.md") {
		t.Errorf("plan:\n%s", v)
	}
}

// TestOilResultsYankedIn: a buffer of results with something yanked
// elsewhere counts only what was found, shows it again after ctrl+r only
// if put; y on the put line makes a task of the copy, and leaves OIL for
// the search (a buffer still holding the copy line, while the copy is
// under way, would plan it again on the next :w).
func TestOilResultsYankedIn(t *testing.T) {
	m, root, _ := writeModel(t)
	goTo(t, m, "README.md")
	typeText(m, "yy")
	key(m, tea.KeyEscape, 0)
	tag(m, ".go")
	key(m, 'o', tea.ModCtrl)
	if m.oil == nil || m.oil.search == nil {
		t.Fatal("ctrl+o did not edit the results")
	}
	if v := m.View().Content; !strings.Contains(v, "2 found") {
		t.Errorf("header counts what was yanked in:\n%s", v)
	}
	key(m, 'r', tea.ModCtrl)
	if got := m.oilTexts(); slices.Contains(got, "README.md") {
		t.Fatalf("ctrl+r shows what was only yanked: %q", got)
	}
	typeText(m, "p")
	typeText(m, "ccsrc/notes.md")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")
	if !exists(filepath.Join(root, "src", "notes.md")) || m.oil != nil || m.notice != "copied 1" {
		t.Errorf("copy into results: in OIL %v, notice %q", m.oil != nil, m.notice)
	}
}
