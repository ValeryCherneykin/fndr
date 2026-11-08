package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustList(t *testing.T, dir string) []indexer.Entry {
	t.Helper()
	es, err := indexer.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func TestCtrlVTogglesPreview(t *testing.T) {
	m := testModel(t, "/r/a.go")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 10})

	if m.paneShown() {
		t.Fatal("preview is on before ctrl+v; it must be off by default")
	}
	key(m, 'v', tea.ModCtrl)
	if !m.paneShown() || m.listWidth() != 60 {
		t.Errorf("after ctrl+v: shown %v, list width %d; want the left half, 60", m.paneShown(), m.listWidth())
	}
	key(m, 'v', tea.ModCtrl)
	if m.paneShown() {
		t.Error("a second ctrl+v did not switch the preview off")
	}
}

func TestPreviewNeedsRoom(t *testing.T) {
	m := testModel(t, "/r/a.go")
	key(m, 'v', tea.ModCtrl)

	for width, shown := range map[int]bool{99: false, 100: true, 101: true} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 10})
		if m.paneShown() != shown {
			t.Errorf("width %d: pane shown %v, want %v", width, m.paneShown(), shown)
		}
	}
}

func TestPreviewLayout(t *testing.T) {
	m := testModel(t, "/r/a.go", "/r/b.go")
	key(m, 'v', tea.ModCtrl)
	m.Update(tea.WindowSizeMsg{Width: 101, Height: 8})

	if !m.paneShown() {
		t.Fatal("pane not shown at width 101 with preview on")
	}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if len(lines) != 8 {
		t.Fatalf("%d lines, want 8", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 101 {
			t.Errorf("line %d is %d cells wide, want 101: %q", i, w, l)
		}
	}
	lw := m.listWidth()
	if got := []rune(lines[1])[lw]; got != '┬' {
		t.Errorf("hairline has %q at the split, want ┬", got)
	}
	for i := 2; i < len(lines)-1; i++ {
		if got := []rune(lines[i])[lw]; got != '│' {
			t.Errorf("line %d has %q at the split, want │", i, got)
		}
	}
}

func TestPaneLoadsSelection(t *testing.T) {
	m, root := browseModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.selectName(t, "src")
	key(m, 'v', tea.ModCtrl) // runs the timer and the build, as the runtime would

	got, ok := m.paneCache[filepath.Join(root, "src")]
	if !ok || got.err != nil || len(got.p.Entries) != 2 {
		t.Errorf("pane for src = %+v (cached %v), want its two files", got, ok)
	}
}

func TestStalePaneTickIsDropped(t *testing.T) {
	m := testModel(t, "/r/a.go", "/r/b.go")
	m.paneWant = "/r/b.go" // the user has moved on from a.go

	if cmd := m.paneTick(paneTickMsg{path: "/r/a.go"}); cmd != nil {
		t.Error("a tick for a row no longer selected still reads the disk")
	}
	if cmd := m.paneTick(paneTickMsg{path: "/r/b.go"}); cmd == nil {
		t.Error("a tick for the selected row reads nothing")
	}
}

func TestHiddenPaneReadsNothing(t *testing.T) {
	m, _ := browseModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	key(m, 'n', tea.ModCtrl)
	key(m, 'n', tea.ModCtrl)

	if m.paneWant != "" || m.paneCache != nil {
		t.Errorf("preview off, yet want %q, cache %v", m.paneWant, m.paneCache)
	}
}

// paneShows returns the pane's side of the screen, one string per list line.
func paneShows(t *testing.T, m *Model) []string {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	lw := m.listWidth()
	body := lines[2 : len(lines)-1]
	out := make([]string, len(body))
	for i, l := range body {
		out[i] = strings.TrimSpace(string([]rune(l)[lw+1:]))
	}
	return out
}

func TestPaneDrawsFolder(t *testing.T) {
	m, _ := browseModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.selectName(t, "src")
	key(m, 'v', tea.ModCtrl)

	got := paneShows(t, m)
	if !strings.HasSuffix(got[0], "main.go") || !strings.HasSuffix(got[1], "util.go") {
		t.Errorf("pane = %q, want src's files", got[:3])
	}
}

func TestPaneDrawsText(t *testing.T) {
	m, root := browseModel(t, "")
	write(t, filepath.Join(root, "README.md"), "# fndr\n\tindented\n")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.selectName(t, "README.md")
	key(m, 'v', tea.ModCtrl)

	got := paneShows(t, m)
	if got[0] != "# fndr" || got[1] != "indented" {
		t.Errorf("pane = %q, want the file's lines", got[:3])
	}
}

// TestPaneFillsScreenExactly: the layout guarantee holds with the pane on,
// whatever it shows and however narrow the halves get.
func TestPaneFillsScreenExactly(t *testing.T) {
	m, root := browseModel(t, "")
	write(t, filepath.Join(root, "README.md"), "a\tb 日本語 "+strings.Repeat("long ", 80)+"\n")
	key(m, 'v', tea.ModCtrl)
	for _, name := range []string{"src", "README.md", "docs"} {
		for _, size := range [][2]int{{100, 6}, {101, 30}, {240, 50}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.selectName(t, name)
			send(m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModAlt}) // a no-op key: lets the pane load
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) != size[1] {
				t.Fatalf("%s at %v: %d lines", name, size, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != size[0] {
					t.Errorf("%s at %v: line %d is %d cells: %q", name, size, i, w, ansi.Strip(l))
				}
			}
		}
	}
}

func TestPaneCard(t *testing.T) {
	m, root := browseModel(t, "")
	write(t, filepath.Join(root, "doc.pdf"), "%PDF-1.7\n\x00\x01")
	send(m, listMsg{dir: root, entries: mustList(t, root)})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.selectName(t, "doc.pdf")
	key(m, 'v', tea.ModCtrl)

	got := paneShows(t, m)
	if got[1] != "PDF document" || got[2] != "11 B" {
		t.Errorf("card = %q, want type and size", got[:4])
	}
}

func TestHumanSize(t *testing.T) {
	tests := map[int64]string{
		0:             "0 B",
		999:           "999 B",
		1000:          "1 KB",
		4200:          "4.2 KB",
		1_300_000:     "1.3 MB",
		5_000_000_000: "5 GB",
	}
	for n, want := range tests {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}
