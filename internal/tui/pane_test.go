package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

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
