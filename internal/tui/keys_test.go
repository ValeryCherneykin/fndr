package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/config"
)

// TestEveryActionIsDone: every action the config names is done here, and
// nothing is done here that the config cannot name: a typo on either side
// would be a key that silently does nothing.
func TestEveryActionIsDone(t *testing.T) {
	named := map[string]bool{}
	for _, a := range config.Actions {
		named[a.Name] = true
		if _, ok := actions[a.Name]; !ok && !slices.Contains(lineActions, a.Name) {
			t.Errorf("%s: no one does it", a.Name)
		}
	}
	for name := range actions {
		if !named[name] {
			t.Errorf("%s is done, but the config cannot name it", name)
		}
	}
	for _, name := range lineActions {
		if !named[name] {
			t.Errorf("%s is the line's, but the config cannot name it", name)
		}
	}
}

// keysModel is browseModel with the config's lines applied.
func keysModel(t *testing.T, lines string) *Model {
	t.Helper()
	m, _ := browseModel(t, "")
	cfg, errs := config.Parse(strings.NewReader(lines))
	m.useConfig(&cfg, errs)
	return m
}

// TestRebind: an action's line is all its keys. The key written moves the
// selection, the old one is free, and an action turned off does nothing.
func TestRebind(t *testing.T) {
	m := keysModel(t, "key.down = ctrl+j\nkey.reveal = none\n")
	if m.notice == "" || !m.noticeErr {
		t.Error("ctrl+j taken from preview_down, and not said")
	}
	key(m, 'n', tea.ModCtrl)
	if m.cursor != 0 {
		t.Errorf("ctrl+n still moves: cursor %d", m.cursor)
	}
	key(m, 'j', tea.ModCtrl)
	if m.cursor != 1 {
		t.Errorf("ctrl+j does not move: cursor %d", m.cursor)
	}
	if cmd := m.update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}); cmd != nil {
		t.Error("ctrl+g reveals, turned off") // the command is looked at, never run: it would open Finder
	}
}

// TestRebindLine: the query line's keys move with the config too, in the
// query and in the path ctrl+r edits.
func TestRebindLine(t *testing.T) {
	m := keysModel(t, "key.cursor_left = alt+b\nkey.delete_word = alt+backspace\nkey.clear = ctrl+x\n")
	typeText(m, "ab cd")
	key(m, 'h', tea.ModCtrl) // free now: nothing
	key(m, 'b', tea.ModAlt)
	if m.qpos != 4 {
		t.Errorf("alt+b: cursor at %d, want 4", m.qpos)
	}
	typeText(m, "x")
	key(m, tea.KeyBackspace, tea.ModAlt)
	if m.query != "ab d" {
		t.Errorf("alt+backspace: %q", m.query)
	}
	key(m, 'u', tea.ModCtrl) // free now: nothing
	if m.query != "ab d" {
		t.Errorf("ctrl+u still erases: %q", m.query)
	}
	key(m, 'x', tea.ModCtrl)
	if m.query != "d" {
		t.Errorf("ctrl+x: %q", m.query)
	}

	m = keysModel(t, "key.cursor_right = alt+f\n")
	m.selectName(t, "README.md")
	key(m, 'r', tea.ModCtrl)
	m.rename.line.pos = 0
	key(m, 'f', tea.ModAlt)
	if m.rename == nil || m.rename.line.pos != 1 {
		t.Errorf("alt+f in the rename line: %+v", m.rename)
	}
}

// TestRebindParent: parent given ctrl+b, "-" is a dash again, typed into
// the empty query; ctrl+b goes up. A shifted letter, a key that types, is
// found by the config's name for it, and goes up only with nothing typed.
func TestRebindParent(t *testing.T) {
	shiftH := tea.KeyPressMsg{Code: 'h', Mod: tea.ModShift, Text: "H"}
	m := keysModel(t, "key.parent = ctrl+b shift+h\n")
	sub := filepath.Join(m.cwd, "src")
	run(m, m.chdir(sub, ""))
	typeText(m, "-")
	if m.query != "-" || m.cwd != sub {
		t.Errorf("-: query %q, cwd %s", m.query, m.cwd)
	}
	send(m, shiftH)
	if m.query != "-H" {
		t.Errorf("shift+h in a query: %q", m.query)
	}
	key(m, tea.KeyBackspace, 0)
	key(m, tea.KeyBackspace, 0)
	key(m, 'b', tea.ModCtrl)
	if m.cwd == sub {
		t.Error("ctrl+b did not go up")
	}
	run(m, m.chdir(sub, ""))
	send(m, shiftH)
	if m.cwd == sub || m.query != "" {
		t.Errorf("shift+h on an empty query: cwd %s, query %q", m.cwd, m.query)
	}
}

// TestBindOnFreedKey: a chord may start on a key an action gave up, and
// not on one an action has, which is named.
func TestBindOnFreedKey(t *testing.T) {
	m := keysModel(t, "key.down = down\nbind = ctrl+n = tag:dirs\nbind = ctrl+p = tag:dirs\n")
	if len(m.binds) != 1 || m.notice != "config line 3: ctrl+p is key.up's" {
		t.Errorf("binds %d, notice %q", len(m.binds), m.notice)
	}
	key(m, 'n', tea.ModCtrl)
	if got := m.tagTexts(); !slices.Equal(got, []string{"dirs"}) {
		t.Errorf("ctrl+n: tags %q", got)
	}
}

// TestMoveKeys: a page at a time, and to either end.
func TestMoveKeys(t *testing.T) {
	m, _ := browseModel(t, "")
	m.results = nil
	for i := range 30 {
		m.results = append(m.results, result(fmt.Sprintf("/r/f%02d", i)))
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10}) // 7 rows
	for _, step := range []struct {
		key  rune
		want int
	}{{tea.KeyPgDown, 7}, {tea.KeyPgDown, 14}, {tea.KeyPgUp, 7}, {tea.KeyEnd, 29}, {tea.KeyHome, 0}} {
		key(m, step.key, 0)
		if m.cursor != step.want {
			t.Errorf("after %v: cursor %d, want %d", step.key, m.cursor, step.want)
		}
	}
}
