package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/oil"
)

// oilModel is browseModel in OIL mode, with a hidden file beside the
// usual tree: README.md, docs/, src/, .git/, .env.
func oilModel(t *testing.T) (m *Model, root string) {
	t.Helper()
	m, root = browseModel(t, "")
	if err := os.WriteFile(filepath.Join(root, ".env"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	key(m, 'o', tea.ModCtrl)
	if m.oil == nil {
		t.Fatal("ctrl+o did not enter OIL")
	}
	return m, root
}

// oilTexts returns the buffer's lines.
func (m *Model) oilTexts() []string {
	ls := m.oil.buf.Lines()
	ts := make([]string, len(ls))
	for i, l := range ls {
		ts[i] = l.Text
	}
	return ts
}

// command types a command line and runs it: command(m, "q!").
func command(m *Model, cmd string) {
	typeText(m, ":"+cmd)
	key(m, tea.KeyEnter, 0)
}

// TestOilShowsFolder: the buffer is the folder as Oil shows it: folders
// first with a trailing "/", hidden names and .git left out.
func TestOilShowsFolder(t *testing.T) {
	m, _ := oilModel(t)
	if got, want := m.oilTexts(), []string{"docs/", "src/", "README.md"}; !slices.Equal(got, want) {
		t.Errorf("buffer %q, want %q", got, want)
	}
	view := m.View()
	for _, s := range []string{" NORMAL ", "README.md", "src/"} {
		if !strings.Contains(view.Content, s) {
			t.Errorf("view lacks %q", s)
		}
	}
	lines := strings.Split(view.Content, "\n")
	if len(lines) != m.height {
		t.Errorf("%d lines, want %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.width {
			t.Errorf("line %d is %d cells, want %d", i, w, m.width)
		}
	}
}

// TestOilLeavesOutWhatItHides: what the buffer does not show must not be
// in the plan's items either, or its missing line would read as "delete
// it" and :w would trash .git.
func TestOilLeavesOutWhatItHides(t *testing.T) {
	m, _ := oilModel(t)
	for _, it := range m.oil.items {
		if it.Name == ".git/" || it.Name == ".env" {
			t.Errorf("item %q is hidden, yet the plan would see it", it.Name)
		}
	}
	if m.oil.changes != 0 {
		t.Errorf("%d changes in a buffer nobody touched", m.oil.changes)
	}
}

func TestOilQuit(t *testing.T) {
	m, _ := oilModel(t)
	command(m, "q")
	if m.oil != nil {
		t.Error(":q on an untouched buffer did not leave OIL")
	}
}

// TestOilQuitRefusesChanges: :q never drops an edit; :q! does, on
// purpose. An edit undone is no edit.
func TestOilQuitRefusesChanges(t *testing.T) {
	m, _ := oilModel(t)
	typeText(m, "dd")
	if m.oil.changes != 1 || !strings.Contains(m.View().Content, "1 change") {
		t.Errorf("changes %d; the status bar should say 1 change", m.oil.changes)
	}
	command(m, "q")
	if m.oil == nil || !m.noticeErr {
		t.Fatal(":q left OIL with a change unwritten")
	}

	typeText(m, "u")
	command(m, "q")
	if m.oil != nil {
		t.Fatal(":q after undoing everything did not leave")
	}

	key(m, 'o', tea.ModCtrl)
	typeText(m, "dd")
	command(m, "q!")
	if m.oil != nil {
		t.Error(":q! did not leave")
	}
}

// TestOilTyping: keys reach the buffer as Vim keys, and the mode shows.
func TestOilTyping(t *testing.T) {
	m, _ := oilModel(t)
	typeText(m, "GA.bak")
	if m.oil.buf.Mode() != oil.Insert || !strings.Contains(m.View().Content, " INSERT ") {
		t.Error("A did not start typing")
	}
	key(m, tea.KeyEscape, 0)
	if got := m.oilTexts()[2]; got != "README.md.bak" {
		t.Errorf("line %q, want README.md.bak", got)
	}
	if m.oil == nil {
		t.Fatal("esc left OIL: it should only leave Insert mode")
	}

	// The terminal's cursor is on the buffer's: after the name's last
	// character, on the third line.
	c := m.View().Cursor
	if c.Y != 2+2 {
		t.Errorf("cursor on screen row %d, want 4", c.Y)
	}
}

func TestOilBadCommand(t *testing.T) {
	m, _ := oilModel(t)
	command(m, "nope")
	if !m.noticeErr || !strings.Contains(m.notice, "nope") {
		t.Errorf("notice %q, want an error naming the command", m.notice)
	}
}

// TestOilScrolls: in a folder taller than the screen the cursor stays in
// view, with lines around it.
func TestOilScrolls(t *testing.T) {
	m, root := oilModel(t)
	for i := range 60 {
		if err := os.WriteFile(filepath.Join(root, "f"+string(rune('a'+i%26))+strings.Repeat("x", i/26)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command(m, "q")
	key(m, 'o', tea.ModCtrl)
	typeText(m, "G")
	h, row := m.listHeight(), m.oil.buf.Row()
	if row < m.oil.offset || row >= m.oil.offset+h {
		t.Errorf("cursor row %d off screen [%d, %d)", row, m.oil.offset, m.oil.offset+h)
	}
	typeText(m, "gg")
	if m.oil.offset != 0 {
		t.Errorf("offset %d at the top", m.oil.offset)
	}
}

// TestOilEveryScreenSize: in every mode and at every size, the screen is
// exactly height lines of exactly width cells, and nothing panics.
func TestOilEveryScreenSize(t *testing.T) {
	for _, keys := range []string{"", "GA.bak", ":wq", "dd"} {
		m, _ := oilModel(t)
		typeText(m, keys)
		for w := 1; w <= 100; w++ {
			for h := 4; h <= 12; h++ {
				m.Update(tea.WindowSizeMsg{Width: w, Height: h})
				lines := strings.Split(m.View().Content, "\n")
				if len(lines) != h {
					t.Fatalf("%q %dx%d: %d lines", keys, w, h, len(lines))
				}
				for i, l := range lines {
					if lw := ansi.StringWidth(l); lw != w {
						t.Fatalf("%q %dx%d: line %d is %d cells", keys, w, h, i, lw)
					}
				}
			}
		}
	}
}
