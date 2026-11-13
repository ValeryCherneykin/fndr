package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/clipboard"
)

// fakeCopier copies into a file instead of the clipboard of whoever runs
// the tests, and returns what it holds.
func fakeCopier(t *testing.T, m *Model) func() string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "clipboard")
	m.copier = copier{tools: []clipboard.Tool{{Name: "fake", Argv: []string{"sh", "-c", "cat > " + file}}}}
	return func() string {
		b, _ := os.ReadFile(file) //nolint:errcheck // nothing copied reads as ""
		return string(b)
	}
}

// yank presses ctrl+y, then k.
func yank(m *Model, k rune, mod tea.KeyMod) {
	key(m, 'y', tea.ModCtrl)
	key(m, k, mod)
}

// TestCopyChord: ctrl+y shows the choices; y copies the full path, r the
// path from the folder searched, n the name, with ctrl held or not.
func TestCopyChord(t *testing.T) {
	m := tagModel(t)
	clip := fakeCopier(t, m)
	typeText(m, "util")
	key(m, 'y', tea.ModCtrl)
	if m.notice != copyHint || clip() != "" {
		t.Fatalf("ctrl+y: notice %q, copied %q; want the choices, nothing yet", m.notice, clip())
	}
	key(m, 'y', 0)
	if clip() != "/r/a/util.go" || m.noticeErr || m.notice != "copied /r/a/util.go · fake" {
		t.Errorf("ctrl+y y: copied %q, notice %q", clip(), m.notice)
	}
	for _, tc := range []struct {
		k    rune
		mod  tea.KeyMod
		want string
	}{
		{'r', 0, "a/util.go"},
		{'n', 0, "util.go"},
		{'y', tea.ModCtrl, "/r/a/util.go"}, // ctrl held all along
		{'r', tea.ModCtrl, "a/util.go"},
	} {
		yank(m, tc.k, tc.mod)
		if clip() != tc.want {
			t.Errorf("ctrl+y %q (mod %v): copied %q, want %q", tc.k, tc.mod, clip(), tc.want)
		}
	}
	if m.query != "util" || m.rename != nil {
		t.Errorf("query %q, rename %v: a chord's keys are the chord's, not typed, and ctrl+r there is no rename", m.query, m.rename != nil)
	}
	key(m, 'r', tea.ModCtrl) // alone, ctrl+r still renames
	if m.rename == nil {
		t.Error("ctrl+r alone: no rename")
	}
}

// TestCopyChordGivesUp: Esc ends the chord and nothing else (no step
// back); a key that copies nothing says so, and is not typed.
func TestCopyChordGivesUp(t *testing.T) {
	m := tagModel(t)
	clip := fakeCopier(t, m)
	typeText(m, "util")
	key(m, 'y', tea.ModCtrl)
	if quit := key(m, tea.KeyEscape, 0); quit || m.query != "util" || m.notice != "" || clip() != "" {
		t.Errorf("esc: quit %v, query %q, notice %q", quit, m.query, m.notice)
	}
	yank(m, 'x', 0)
	if m.query != "util" || !m.noticeErr || !strings.Contains(m.notice, "copies nothing") {
		t.Errorf("ctrl+y x: query %q, notice %q", m.query, m.notice)
	}
	typeText(m, "s") // and typing goes on
	if m.query != "utils" {
		t.Errorf("after: query %q", m.query)
	}
	key(m, 'y', tea.ModCtrl)
	send(m, tea.PasteMsg{Content: "x"}) // a paste is typing: the chord is over
	typeText(m, "r")
	if m.query != "utilsxr" || clip() != "" {
		t.Errorf("ctrl+y, paste, r: query %q, copied %q", m.query, clip())
	}
}

// TestCopyNothingSelected: with nothing in the list, the folder is what
// is copied; it has no path below itself to give, and says so.
func TestCopyNothingSelected(t *testing.T) {
	m := tagModel(t)
	clip := fakeCopier(t, m)
	typeText(m, "zzz")
	yank(m, 'y', 0)
	if clip() != "/r" {
		t.Errorf("ctrl+y y on nothing: %q, want the folder", clip())
	}
	yank(m, 'r', 0)
	if !m.noticeErr || !strings.Contains(m.notice, "nothing below") {
		t.Errorf("ctrl+y r on nothing: notice %q", m.notice)
	}
}

// TestCopyNeverSilent: no way to copy, or a way that fails, is said in
// red; a copy that seems to work and does nothing is the worst outcome.
func TestCopyNeverSilent(t *testing.T) {
	m := tagModel(t)
	m.copier = copier{}
	yank(m, 'y', 0)
	if !m.noticeErr || !strings.Contains(m.notice, "no clipboard") {
		t.Errorf("no way: notice %q", m.notice)
	}
	m.copier = copier{tools: []clipboard.Tool{{Name: "broken", Argv: []string{"sh", "-c", "exit 1"}}}}
	yank(m, 'y', 0)
	if !m.noticeErr || !strings.Contains(m.notice, "cannot copy: broken") {
		t.Errorf("a broken way: notice %q", m.notice)
	}
}

// TestCopyOverSSH: over SSH the terminal is asked to copy (OSC 52), and
// the status says so.
func TestCopyOverSSH(t *testing.T) {
	m := tagModel(t)
	m.copier = copier{osc52: true}
	yank(m, 'y', 0)
	if m.noticeErr || !strings.Contains(m.notice, "· osc52") {
		t.Errorf("notice %q", m.notice)
	}
}

// TestOilCopiesPath: gp in OIL copies the full path of the line.
func TestOilCopiesPath(t *testing.T) {
	m, root := oilModel(t)
	clip := fakeCopier(t, m)
	typeText(m, "gp")
	if want := filepath.Join(root, "docs"); clip() != want {
		t.Errorf("gp: copied %q, want %q", clip(), want)
	}
}
