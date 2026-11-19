package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

func TestTutorCourse(t *testing.T) {
	m, err := NewTutor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	m.graphics, m.probeGraphics = graphicsNone, false
	m.tickEvery = 0
	for _, msg := range runCmd(m.Init()) {
		send(m, msg)
	}
	send(m, tea.WindowSizeMsg{Width: 110, Height: 27})

	check := func(title string) {
		t.Helper()
		if m.tutor.step >= len(tutorLessons) || tutorLessons[m.tutor.step].title != title {
			t.Fatalf("want lesson %q, step=%d, notice=%q, query=%q, matches=%q", title, m.tutor.step+1, m.notice, m.query, m.names())
		}
	}
	press := func(code rune) { key(m, code, 0) }
	ctrl := func(code rune) { key(m, code, tea.ModCtrl) }
	addTag := func(s string) {
		typeText(m, s)
		ctrl('t')
	}

	check("Search · Find a file")
	typeText(m, "fldnt")
	check("Search · Edit the query")
	press(tea.KeyBackspace)
	check("Browse · Enter a folder")
	press(tea.KeyEscape)
	typeText(m, "docs/")
	press(tea.KeyEnter)
	check("Browse · Move the selection")
	press(tea.KeyDown)
	check("Preview · Read before opening")
	press(tea.KeyUp)
	ctrl('v')
	check("Preview · Scroll the file")
	ctrl('j')
	check("Open · Edit a text file")
	send(m, openedMsg{name: "field-notes.md"})
	check("Browse · Go back")
	typeText(m, "-")
	check("Filter · File extension")
	addTag(".go")
	addTag("atlas")
	press(tea.KeyEscape)
	ctrl('u')
	addTag("doc")
	ctrl('u')
	addTag("dirs")
	ctrl('u')
	addTag("size>100b")
	ctrl('u')
	addTag("@today")
	ctrl('u')
	addTag(".go|dirs")
	ctrl('u')
	addTag("has:.json")
	addTag("has:dockerfile")
	check("Scope · Search globally")
	ctrl('u')
	typeText(m, "docs/")
	press(tea.KeyEnter)
	typeText(m, "main")
	press(tea.KeyTab)
	check("Scope · Include ignored files")
	press(tea.KeyTab)
	press(tea.KeyEscape)
	typeText(m, "-generated")
	if len(m.results) != 0 {
		t.Fatal("found generated.log before opting into ignored files")
	}
	ctrl('d')
	check("Paths · Copy what you found")
	send(m, copiedMsg{text: "generated.log", copied: []string{"test"}})
	press(tea.KeyF6)
	check("Change · Rename a file")
	ctrl('d')
	press(tea.KeyEscape)
	typeText(m, "draft")
	ctrl('r')
	retype(m, "practice/ready.txt")
	check("Change · Undo on disk")
	ctrl('z')
	typeText(m, "y")
	check("Oil · Edit a folder as text")
	press(tea.KeyEscape)
	typeText(m, "practice/")
	press(tea.KeyEnter)
	ctrl('o')
	check("Oil · Undo a buffer edit")
	typeText(m, "ddu")
	press(tea.KeyF6)
	check("Oil · Create a file")
	typeText(m, "Goideas.md")
	press(tea.KeyEscape)
	command(m, "w")
	typeText(m, "y")
	check("Oil · Copy by duplicating a line")
	typeText(m, "ggjjyyp")
	typeText(m, "ccbackup.txt")
	press(tea.KeyEscape)
	command(m, "w")
	typeText(m, "y")
	check("Oil · Trash a file")
	ctrl('o')
	typeText(m, "Gdd")
	command(m, "w")
	typeText(m, "y")
	check("Oil · Restore a trashed file")
	if !exists(filepath.Join(m.tutor.trash, "spare.txt")) {
		t.Fatal("missing file from the tutor's trash")
	}
	press(tea.KeyEscape)
	ctrl('z')
	typeText(m, "y")
	check("Oil · Edit search results")
	typeText(m, "source")
	ctrl('o')
	check("Oil · More Vim commands")
	press(tea.KeyEscape)
	press(tea.KeyF6)
	press(tea.KeyF6)
	press(tea.KeyF6)
	check("Help · Find any key")
	press(tea.KeyF1)
	if !strings.Contains(ansi.Strip(m.View().Content), "Course complete") {
		t.Fatal("missing completion message")
	}

	for _, size := range []tea.WindowSizeMsg{{Width: 110, Height: 27}, {Width: 45, Height: 12}, {Width: 22, Height: 6}} {
		send(m, size)
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != size.Height {
			t.Errorf("view has %d rows, want %d", len(lines), size.Height)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) != size.Width {
				t.Errorf("line has width %d, want %d", ansi.StringWidth(line), size.Width)
			}
		}
	}
}

func TestTutorWorkspaceBoundary(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	m, err := NewTutor(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.stop()
	if err := os.Symlink(outside, filepath.Join(m.start, "link")); err != nil {
		t.Fatal(err)
	}
	pressUp := m.up()
	if pressUp != nil || m.cwd != m.start {
		t.Fatal("parent navigation escaped the workspace")
	}
	for _, name := range []string{filepath.Join(outside, "file"), "../file", "link/file"} {
		if m.tutorPlan(m.start, mutation.Plan{{Kind: mutation.OpRename, From: ".project", To: name}}) {
			t.Errorf("accepted destination outside the workspace: %q", name)
		}
	}
	if !m.tutorPlan(m.start, mutation.Plan{{Kind: mutation.OpRename, From: ".project", To: "new/file"}}) {
		t.Fatal("rejected a destination inside the workspace")
	}
}
