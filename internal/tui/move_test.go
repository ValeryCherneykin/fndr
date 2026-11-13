package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// awayModel is browseModel in a home of its own: root is ~/Downloads, the
// test's folder, and ~/Documents is next to it, for moves away.
func awayModel(t *testing.T) (m *Model, root, home string) {
	t.Helper()
	m, root = browseModel(t, "")
	home = filepath.Dir(root)
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	m.home = home
	return m, root, home
}

// TestOilMoveAway: a line rewritten as ~/… moves the entry there, as mv;
// the plan shows where, in ~ form, before y.
func TestOilMoveAway(t *testing.T) {
	m, root, home := awayModel(t)
	m.global = &scope{root: home, ix: &indexer.Index{}} // the home's index, where ~/Documents is
	key(m, 'o', tea.ModCtrl)
	goTo(t, m, "src/")
	typeText(m, "cc~/Documents/video/src/")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	if !strings.Contains(m.View().Content, "src/ → ~/Documents/video/src/") {
		t.Errorf("the plan does not say where:\n%s", m.View().Content)
	}
	typeText(m, "y")
	if !exists(filepath.Join(home, "Documents", "video", "src", "main.go")) || exists(filepath.Join(root, "src")) {
		t.Fatalf("not moved: notice %q", m.notice)
	}
	if m.notice != "moved 1" {
		t.Errorf("notice %q, want moved 1", m.notice)
	}
	if exists(filepath.Join(root, "~")) {
		t.Error(`a folder named "~" was made`)
	}
	for _, d := range []string{"Documents", "Documents/video", "Documents/video/src"} {
		found := false
		for _, e := range m.global.ix.Snapshot() {
			found = found || e.Path == filepath.Join(home, d)
		}
		if !found {
			t.Errorf("~/%s was made by the move but the home's index does not know it", d)
		}
	}
	if slices.Contains(m.oilTexts(), "src/") {
		t.Errorf("after y the folder still lists src: %q", m.oilTexts())
	}
}

// TestOilTildeFolderUntouched: a folder named "~" is shown as "~/", and
// left as it is when another line changes: not read as home.
func TestOilTildeFolderUntouched(t *testing.T) {
	m, root, _ := awayModel(t)
	if err := os.Mkdir(filepath.Join(root, "~"), 0o750); err != nil {
		t.Fatal(err)
	}
	key(m, 'o', tea.ModCtrl)
	goTo(t, m, "README.md")
	typeText(m, "ccNOTES.md")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	if len(m.oil.plan) != 1 || m.oil.plan[0].To != "NOTES.md" {
		t.Errorf("plan %v, want the one rename", m.oil.plan)
	}
}

// TestRenameMoveAway: ctrl+r to ~/… moves it, then shows it where it went,
// selected: a search for its name in the folder left would find nothing.
func TestRenameMoveAway(t *testing.T) {
	m, root, home := awayModel(t)
	typeText(m, "util")
	key(m, 'r', tea.ModCtrl)
	retype(m, "~/Documents/util.go")
	to := filepath.Join(home, "Documents", "util.go")
	if !exists(to) || exists(filepath.Join(root, "src", "util.go")) {
		t.Fatalf("not moved: notice %q", m.notice)
	}
	if m.cwd != filepath.Dir(to) || !strings.Contains(m.notice, "moved util.go → ~/Documents/util.go") {
		t.Errorf("cwd %q, notice %q; want ~/Documents and the move said", m.cwd, m.notice)
	}
	send(m, m.load(m.cwd)()) // the listing, read
	if len(m.results) == 0 || m.results[m.cursor].Entry.Path != to {
		t.Errorf("selected %v, want util.go", m.resultPaths())
	}
}
