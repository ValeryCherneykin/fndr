package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ValeryCherneykin/fndr/internal/config"
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
	if m.preview || !m.graphics || m.editor != "" || m.hist == nil || m.savedPath == "" {
		t.Errorf("defaults: preview %v, graphics %v, editor %q, hist %v, saved %q", m.preview, m.graphics, m.editor, m.hist, m.savedPath)
	}

	cfg := config.Default()
	cfg.Preview, cfg.Images, cfg.Editor = true, false, "hx"
	m = New(home, cfg, nil)
	if !m.preview || m.graphics || m.editor != "hx" {
		t.Errorf("set: preview %v, graphics %v, editor %q", m.preview, m.graphics, m.editor)
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
