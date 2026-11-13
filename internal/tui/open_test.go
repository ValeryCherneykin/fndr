package tui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/opener"
)

func TestResolveOpen(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "notes.md")
	binary := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(text, []byte("# notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("\x89PNG\r\n\x1a\n\x00"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Text goes to the editor, in the terminal. A configured editor wins
	// over the environment, which keeps this test independent of the
	// machine.
	got, ok := resolveOpen(text, "ed -s").(openMsg)
	if !ok || !got.terminal || !slices.Equal(got.argv, []string{"ed", "-s", text}) {
		t.Errorf("text file: got %+v, want the editor in the terminal", got)
	}

	// Anything else goes to the system opener, outside the terminal. A CI
	// machine may have none installed; then the error must say so.
	switch msg := resolveOpen(binary, "ed").(type) {
	case openMsg:
		if msg.terminal || msg.argv[len(msg.argv)-1] != binary {
			t.Errorf("binary file: got %+v, want the system opener", msg)
		}
	case openedMsg:
		if !errors.Is(msg.err, opener.ErrNoOpener) {
			t.Errorf("binary file: err = %v, want ErrNoOpener", msg.err)
		}
	}

	// A program is never run: enter on a binary only says what it is.
	program := filepath.Join(dir, "fndr")
	if err := os.WriteFile(program, []byte("\xcf\xfa\xed\xfe\x0c\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if msg, ok := resolveOpen(program, "ed").(openedMsg); !ok || !msg.program {
		t.Errorf("program: got %+v, want it reported, not opened", msg)
	}

	// A file deleted since it was indexed is reported, not ignored.
	if msg, ok := resolveOpen(filepath.Join(dir, "gone.txt"), "ed").(openedMsg); !ok || msg.err == nil {
		t.Errorf("missing file: got %+v, want an error", msg)
	}
}

func TestEnterWithNoResults(t *testing.T) {
	m := testModel(t, "/r/a.go")
	if quit := key(m, tea.KeyEnter, 0); quit || m.notice != "" {
		t.Errorf("enter on an empty list: quit=%v notice=%q, want nothing", quit, m.notice)
	}
}

// TestOpenOutcomeIsVisible: the result of an open is always reported, and
// a failure never silently (CLAUDE.md, "Чего НЕ делать").
func TestOpenOutcomeIsVisible(t *testing.T) {
	m := testModel(t, "/r/a.go")

	m.Update(openedMsg{name: "a.pdf", viaApp: true})
	if status := ansi.Strip(m.viewStatus()); !strings.Contains(status, "opened a.pdf") {
		t.Errorf("status %q, want the opened file named", status)
	}

	m.Update(openedMsg{name: "fndr", program: true})
	if status := ansi.Strip(m.viewStatus()); !strings.Contains(status, "fndr is a program") {
		t.Errorf("status %q, want the program named and not run", status)
	}

	m.Update(openedMsg{name: "a.pdf", err: opener.ErrNoOpener})
	if status := ansi.Strip(m.viewStatus()); !strings.Contains(status, "cannot open a.pdf") {
		t.Errorf("status %q, want the failure reported", status)
	}

	// A long path must not push the notice off a narrow bar.
	m.cwd = "/" + strings.Repeat("very-long-directory/", 10)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m.setNotice("already at the top", false)
	if status := ansi.Strip(m.viewStatus()); !strings.Contains(status, "already at the top") {
		t.Errorf("status %q, want the notice kept over the directory", status)
	}

	key(m, tea.KeyDown, 0)
	if m.notice != "" {
		t.Errorf("notice %q survived a key press", m.notice)
	}
}

// TestOpenDoesNotWaitForTheApp: an opener that starts an app and exits,
// the app keeping its output open, as xdg-open's often do, is done when
// the opener is: the status must not wait until the app is quit.
func TestOpenDoesNotWaitForTheApp(t *testing.T) {
	msg := openMsg{name: "x.pdf", argv: []string{"sh", "-c", "(sleep 5) &"}}
	start := time.Now()
	got, ok := msg.run()().(openedMsg)
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("took %v: waited for the app", took)
	}
	if !ok || got.err != nil || !got.viaApp {
		t.Errorf("%+v: want opened, no error", got)
	}
}
