package tui

import (
	"path/filepath"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/opener"
)

// revealed presses k and returns the command fndr would run to show
// something in the file manager. It is never run: the test must not open
// Finder. That is why Update is called here, not send.
func revealed(t *testing.T, m *Model, keys ...tea.KeyPressMsg) []string {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(k)
	}
	if cmd == nil {
		t.Fatalf("no command; notice %q", m.notice)
	}
	out := cmd()
	msg, ok := out.(openMsg)
	if !ok || !msg.reveal {
		t.Fatalf("%T %#v is not a reveal", out, out)
	}
	return msg.argv
}

func wantReveal(t *testing.T, path string, isDir bool) []string {
	t.Helper()
	argv, err := opener.Reveal(path, isDir)
	if err != nil {
		t.Skip("no file manager here:", err)
	}
	return argv
}

// TestRevealSelected: ctrl+g shows the selected entry, as a file or a
// folder; with nothing in the list, the folder itself.
func TestRevealSelected(t *testing.T) {
	ctrlG := tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}
	m := tagModel(t)
	typeText(m, "util")
	if got, want := revealed(t, m, ctrlG), wantReveal(t, "/r/a/util.go", false); !slices.Equal(got, want) {
		t.Errorf("a file: %q, want %q", got, want)
	}

	m = tagModel(t)
	typeText(m, "zzz")
	if got, want := revealed(t, m, ctrlG), wantReveal(t, "/r", true); !slices.Equal(got, want) {
		t.Errorf("nothing found: %q, want the folder, %q", got, want)
	}
}

// TestRevealSaysShown: once the file manager has it, the status says so.
func TestRevealSaysShown(t *testing.T) {
	m := tagModel(t)
	send(m, openedMsg{name: "util.go", revealed: true})
	if m.noticeErr || m.notice != "shown util.go" {
		t.Errorf("notice %q", m.notice)
	}
}

// TestOilReveal: gx in OIL shows the line's entry.
func TestOilReveal(t *testing.T) {
	m, root := oilModel(t) // docs/ is the first line
	got := revealed(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"}, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if want := wantReveal(t, filepath.Join(root, "docs"), true); !slices.Equal(got, want) {
		t.Errorf("gx: %q, want %q", got, want)
	}
}

// TestRevealFileWithoutExtension: a Makefile is a file, shown selected;
// taken for a folder, open would open it in an application instead.
func TestRevealFileWithoutExtension(t *testing.T) {
	m := testModel(t, "/r/Makefile", "/r/src/main.go")
	typeText(m, "makefile")
	got := revealed(t, m, tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if want := wantReveal(t, "/r/Makefile", false); !slices.Equal(got, want) {
		t.Errorf("%q, want %q", got, want)
	}
}

// TestOilRevealEditedFolder: a folder's line with its "/" erased is still
// a folder: what it is comes from the disk, not from the text.
func TestOilRevealEditedFolder(t *testing.T) {
	m, root := oilModel(t)
	typeText(m, "$x") // docs/ → docs, still in Normal mode
	if m.oil == nil || m.oilTexts()[0] != "docs" {
		t.Fatalf("the edit: %q", m.oilTexts())
	}
	got := revealed(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"}, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if want := wantReveal(t, filepath.Join(root, "docs"), true); !slices.Equal(got, want) {
		t.Errorf("gx on docs: %q, want %q", got, want)
	}
}

// TestEnterStartsApp: Enter on an application starts it, as Finder and
// Spotlight do, on macOS; elsewhere x.app is a folder, and Enter goes in.
// The command is looked at, never run: the test must not start an app.
func TestEnterStartsApp(t *testing.T) {
	m := testModel(t, "/r/main.go")
	app := "/Applications/Ghostty.app"
	m.results = []indexer.Result{{Entry: indexer.NewEntry(app, 0, 0, true)}}
	m.cursor = 0
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	var opened []string
	for _, c := range flatten(cmd) {
		if msg, ok := c().(openMsg); ok {
			opened = msg.argv
		}
	}
	if !opener.IsApp(app, true) {
		if opened != nil || m.cwd != app {
			t.Errorf("not macOS: opened %q, cwd %q; want to go in", opened, m.cwd)
		}
		return
	}
	want, _ := opener.System(app)
	if !slices.Equal(opened, want) || m.cwd != "/r" {
		t.Errorf("opened %q, cwd %q; want %q and fndr where it was", opened, m.cwd, want)
	}
}
