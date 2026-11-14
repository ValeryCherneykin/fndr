package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/config"
	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// tempHome points everything New looks at into a fresh folder: the home,
// where the history and the index live, and a terminal that shows
// pictures. Nothing of the real home is read or written.
func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "/x") // a terminal with pictures
	t.Setenv("TMUX", "")
	return home
}

// TestSettingsAsComeIn: the file's switches reach the interface, the
// defaults too.
func TestSettingsAsComeIn(t *testing.T) {
	home := tempHome(t)
	m := New(home, config.Default(), nil)
	if m.preview || !m.graphics || m.editor != "" || m.hist == nil || m.savedPath == "" || !m.icons {
		t.Errorf("defaults: preview %v, graphics %v, editor %q, hist %v, saved %q, icons %v", m.preview, m.graphics, m.editor, m.hist, m.savedPath, m.icons)
	}

	cfg := config.Default()
	cfg.Preview, cfg.Images, cfg.Editor, cfg.Icons, cfg.EditorGUI = true, false, "code", false, true
	m = New(home, cfg, nil)
	if !m.preview || m.graphics || m.editor != "code" || m.icons {
		t.Errorf("set: preview %v, graphics %v, editor %q, icons %v", m.preview, m.graphics, m.editor, m.icons)
	}
	// The command is looked at, never run: it would open VS Code.
	text := filepath.Join(home, "notes.md")
	if err := os.WriteFile(text, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if msg, ok := m.open(text)().(openMsg); !ok || msg.terminal || !slices.Equal(msg.argv, []string{"code", text}) {
		t.Errorf("editor_gui: %+v, want code started outside the terminal", msg)
	}
}

// TestPrivacyOff: history = off and save_index = off read nothing of what
// is on the disk and will write nothing: the index is crawled, the habits
// are not shown, and what was saved before stays as it was.
func TestPrivacyOff(t *testing.T) {
	home := tempHome(t)
	hist := filepath.Join(home, ".local", "state", "fndr", "frecency")
	if err := os.MkdirAll(filepath.Dir(hist), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hist, []byte("not read\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.History, cfg.SaveIndex = false, false
	m := New(home, cfg, nil)
	if m.hist != nil || m.savedPath != "" {
		t.Fatalf("hist %v, saved %q: want neither", m.hist, m.savedPath)
	}
	if m.learn(home) != nil {
		t.Error("learning with history off")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(hist); string(b) != "not read\n" {
		t.Errorf("history file now %q", b)
	}
	if _, err := os.Stat(filepath.Join(home, ".cache", "fndr")); !os.IsNotExist(err) {
		t.Errorf("index folder made: %v", err)
	}
}

// TestIconsOff: without a Nerd Font no icon is drawn anywhere, in the
// list, the pane or OIL, and a folder is told by a "/" after its name.
func TestIconsOff(t *testing.T) {
	m, root := browseModel(t, "")
	if err := os.Mkdir(filepath.Join(root, "src", "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	glyphs := []string{ansi.Strip(iconFor("src", true)), ansi.Strip(iconFor("main.go", false)), ansi.Strip(iconFor("README.md", false))}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, glyphs[0]) {
		t.Fatalf("icons on, none drawn:\n%s", v)
	}
	m.icons = false
	noIcons := func(where string) {
		t.Helper()
		v := ansi.Strip(m.View().Content)
		for _, g := range glyphs {
			if strings.Contains(v, g) {
				t.Errorf("%s: icon %q drawn:\n%s", where, g, v)
			}
		}
	}

	noIcons("list")
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	row := func(want string) func(string) bool {
		return func(r string) bool { return strings.Trim(r, " ▎") == want }
	}
	if !slices.ContainsFunc(rows, row("docs/")) || !slices.ContainsFunc(rows, row("README.md")) {
		t.Errorf("list rows %q: want docs/ and README.md, no icons", rows)
	}

	m.selectName(t, "src")
	key(m, 'v', tea.ModCtrl)
	noIcons("pane")
	if got := paneShows(t, m); got[0] != "sub/" || got[1] != "main.go" {
		t.Errorf("pane %q", got[:2])
	}

	key(m, 'o', tea.ModCtrl)
	noIcons("OIL")
	if !strings.Contains(ansi.Strip(m.View().Content), " src/") {
		t.Errorf("OIL lacks src/:\n%s", ansi.Strip(m.View().Content))
	}
}

// TestIconsOffRoomForName: the cells the icon took go to the name; a name
// that fits is never cut.
func TestIconsOffRoomForName(t *testing.T) {
	r := result("/r/abcdefg") // 7 cells: marker and space, the name, padding make 10
	if got := ansi.Strip(renderRow(&r, "/r", 10, rowLook{}, &accentLocal)); got != "  abcdefg " {
		t.Errorf("row %q", got)
	}
	r.Entry = indexer.NewEntry("/r/abcdef", 0, 0, true)
	if got := ansi.Strip(renderRow(&r, "/r", 10, rowLook{}, &accentLocal)); got != "  abcdef/ " {
		t.Errorf("folder row %q", got)
	}
}
