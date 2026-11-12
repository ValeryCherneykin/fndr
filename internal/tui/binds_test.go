package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/config"
)

// bindModel is tagModel with the binds of a config file.
func bindModel(t *testing.T, file string) (*Model, []error) {
	t.Helper()
	m := tagModel(t)
	cfg, errs := config.Parse(strings.NewReader(file))
	if len(errs) > 0 {
		t.Fatalf("the file itself: %v", errs)
	}
	errs = m.useBinds(cfg.Binds)
	return m, errs
}

// TestBindChord: ctrl+f says what may follow, f turns the tag on, the
// same chord again takes it off.
func TestBindChord(t *testing.T) {
	m, errs := bindModel(t, "bind = ctrl+f>f = tag:dirs\nbind = ctrl+f>g = tag:.go\n")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	key(m, 'f', tea.ModCtrl)
	if !strings.Contains(m.notice, "f dirs") || !strings.Contains(m.notice, "g .go") || m.query != "" {
		t.Errorf("waiting: notice %q, query %q; want the choices said, nothing typed", m.notice, m.query)
	}
	key(m, 'f', 0)
	if !slices.Equal(m.tagTexts(), []string{"dirs"}) || m.query != "" {
		t.Fatalf("ctrl+f f: tags %q, query %q; want dirs, nothing typed", m.tagTexts(), m.query)
	}
	if got := m.names(); !slices.Equal(got, []string{"dir"}) {
		t.Errorf("searched: %q, want the folder", got)
	}
	key(m, 'f', tea.ModCtrl)
	key(m, 'f', 0)
	if len(m.tags) != 0 {
		t.Errorf("the chord again: tags %q, want none", m.tagTexts())
	}
}

// TestBindLayout: the second key is the key, not its letter: on a Russian
// layout, the key of f types а.
func TestBindLayout(t *testing.T) {
	m, _ := bindModel(t, "bind = ctrl+f>f = tag:dirs\n")
	key(m, 'f', tea.ModCtrl)
	send(m, tea.KeyPressMsg{Code: 'а', BaseCode: 'f', Text: "а"})
	if !slices.Equal(m.tagTexts(), []string{"dirs"}) {
		t.Errorf("tags %q, want dirs", m.tagTexts())
	}
}

// TestBindGivesUp: Esc stops waiting and does nothing else; a key that
// follows nothing bound says so and is not typed.
func TestBindGivesUp(t *testing.T) {
	m, _ := bindModel(t, "bind = ctrl+f>f = tag:dirs\n")
	typeText(m, "ma")
	key(m, 'f', tea.ModCtrl)
	if quit := key(m, tea.KeyEscape, 0); quit || m.query != "ma" || len(m.pending) != 0 || m.notice != "" {
		t.Errorf("esc: quit %v, query %q, pending %q, notice %q; want nothing but the wait undone", quit, m.query, m.pending, m.notice)
	}
	key(m, 'f', tea.ModCtrl)
	typeText(m, "x") // not typed: it answers the wait
	if m.query != "ma" || !m.noticeErr || !strings.Contains(m.notice, "ctrl+f>x is not bound") {
		t.Errorf("ctrl+f x: query %q, notice %q", m.query, m.notice)
	}
	typeText(m, "x") // and typing goes on as usual
	if m.query != "max" {
		t.Errorf("after: query %q, want max", m.query)
	}
}

// TestBindRefused: binds that would take a key away, or do nothing, are
// refused with their line, and the others still work.
func TestBindRefused(t *testing.T) {
	m, errs := bindModel(t, `bind = f = tag:dirs
bind = shift+f = tag:dirs
bind = ctrl+n = tag:dirs
bind = ctrl+f>f = tag:dirs
bind = ctrl+f = tag:.go
bind = ctrl+f>f>g = tag:.go
bind = ctrl+f>f = tag:.go
bind = ctrl+g = tag:dirs>1mb
bind = alt+d = tag:dirs
bind = f5 = tag:.go
bind = f1 = tag:.go
`)
	lines := make([]int, 0, len(errs))
	for _, err := range errs {
		var e *config.Error
		if !errors.As(err, &e) {
			t.Fatalf("%v is not a *config.Error", err)
		}
		lines = append(lines, e.Line)
	}
	if !slices.Equal(lines, []int{1, 2, 3, 5, 6, 7, 8, 11}) {
		t.Errorf("refused lines %v: %v", lines, errs)
	}
	if len(m.binds) != 3 {
		t.Errorf("%d binds kept, want ctrl+f>f, alt+d, f5", len(m.binds))
	}
	key(m, 'd', tea.ModAlt)
	if !slices.Equal(m.tagTexts(), []string{"dirs"}) {
		t.Errorf("alt+d: tags %q", m.tagTexts())
	}
}

// TestBindOnlyWhileSearching: OIL and the rename line have keys of their
// own; a bind never takes theirs.
func TestBindOnlyWhileSearching(t *testing.T) {
	m, _ := browseModel(t, "")
	cfg, _ := config.Parse(strings.NewReader("bind = ctrl+f>f = tag:dirs\n"))
	m.useBinds(cfg.Binds)
	typeText(m, "util")
	key(m, 'r', tea.ModCtrl) // rename util.go
	key(m, 'f', tea.ModCtrl)
	typeText(m, "f")
	if len(m.tags) != 0 || m.rename == nil || !strings.HasSuffix(m.rename.line.text, "f") {
		t.Errorf("in rename: tags %q, line %+v; want the f typed into the line", m.tagTexts(), m.rename)
	}
}

// TestConfigMistakesSaid: the file's mistakes and the interface's are
// said together, the first in full, the rest counted.
func TestConfigMistakesSaid(t *testing.T) {
	m := tagModel(t)
	cfg, errs := config.Parse(strings.NewReader("preview = on\nbind = ctrl+n = tag:dirs\nbind = alt+d = tag:dirs\n"))
	m.useConfig(cfg, errs)
	if !m.noticeErr || m.notice != `config line 1: unknown setting "preview" (and 1 more)` {
		t.Errorf("notice %q (err %v)", m.notice, m.noticeErr)
	}
	if len(m.binds) != 1 {
		t.Errorf("%d binds, want alt+d", len(m.binds))
	}
	m = tagModel(t)
	m.useConfig(config.Config{}, nil)
	if m.notice != "" {
		t.Errorf("no config, notice %q", m.notice)
	}
}

// TestBindCtrlHeld: ctrl+f>f pressed with ctrl held all along, ctrl+ff,
// is the same chord; unless ctrl+f>ctrl+f is bound itself.
func TestBindCtrlHeld(t *testing.T) {
	m, _ := bindModel(t, "bind = ctrl+f>f = tag:dirs\nbind = ctrl+f>g = tag:.go\n")
	key(m, 'f', tea.ModCtrl)
	key(m, 'f', tea.ModCtrl)
	if !slices.Equal(m.tagTexts(), []string{"dirs"}) {
		t.Errorf("ctrl+f ctrl+f: tags %q, want dirs", m.tagTexts())
	}

	m, _ = bindModel(t, "bind = ctrl+f>f = tag:dirs\nbind = ctrl+f>ctrl+f = tag:.go\n")
	key(m, 'f', tea.ModCtrl)
	key(m, 'f', tea.ModCtrl)
	if !slices.Equal(m.tagTexts(), []string{".go"}) {
		t.Errorf("both bound, ctrl+f ctrl+f: tags %q, want .go", m.tagTexts())
	}
}
