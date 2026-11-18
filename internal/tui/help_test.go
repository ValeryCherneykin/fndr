package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestHelpIsTheConfig: the help page says what the keys do as the config
// left them: a key moved shows where it went, an action turned off says
// so, the user's chords are there, and the help key itself is the one
// configured.
func TestHelpIsTheConfig(t *testing.T) {
	m := keysModel(t, "key.down = ctrl+j\nkey.reveal = none\nkey.help = f2\nbind = ctrl+f>f = tag:dirs\n")
	m.home = "/home/me"
	m.SetConfigPath("/home/me/.config/fndr/config", false)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 200}) // the whole page at once
	key(m, tea.KeyF1, 0)
	if m.help != nil {
		t.Fatal("f1 opens help, given f2")
	}
	key(m, tea.KeyF2, 0)
	if m.help == nil {
		t.Fatal("f2 does not open help")
	}
	v := ansi.Strip(m.View().Content)
	for _, want := range []string{
		"ctrl+j                Move down the list.",
		"(off)                 Show the selected entry",
		"ctrl+f>f              tag dirs",
		"f2                    The help page.",
		"~/.config/fndr/config",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("help lacks %q:\n%s", want, v)
		}
	}
	key(m, tea.KeyF2, 0)
	if m.help != nil {
		t.Error("f2 does not close help")
	}
}

// TestHelpScrolls: j and the keys for moving scroll the page, never past
// its ends; esc and q close it, and the search is as it was.
func TestHelpScrolls(t *testing.T) {
	m, _ := browseModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	typeText(m, "rea")
	key(m, tea.KeyF1, 0)
	total, page := len(m.help.lines), m.listHeight()
	typeText(m, "jj")
	key(m, 'n', tea.ModCtrl)
	if m.help.scroll != 3 {
		t.Errorf("scroll %d, want 3", m.help.scroll)
	}
	typeText(m, "G")
	if m.help.scroll != total-page {
		t.Errorf("G: scroll %d, want %d", m.help.scroll, total-page)
	}
	typeText(m, "j")
	if m.help.scroll != total-page {
		t.Errorf("past the end: scroll %d", m.help.scroll)
	}
	typeText(m, "g")
	typeText(m, "k")
	if m.help.scroll != 0 {
		t.Errorf("before the start: scroll %d", m.help.scroll)
	}
	key(m, tea.KeyPgDown, 0)
	if m.help.scroll != page {
		t.Errorf("pgdown: scroll %d, want %d", m.help.scroll, page)
	}
	typeText(m, "q")
	if m.help != nil || m.query != "rea" {
		t.Errorf("q: help %v, query %q", m.help, m.query)
	}
	key(m, tea.KeyF1, 0)
	key(m, tea.KeyEscape, 0)
	if m.help != nil || m.query != "rea" {
		t.Errorf("esc: help %v, query %q", m.help, m.query)
	}
}

// TestHelpFillsScreenExactly: every size, exactly height lines of exactly
// width cells, at every scroll.
func TestHelpFillsScreenExactly(t *testing.T) {
	m, _ := browseModel(t, "")
	key(m, tea.KeyF1, 0)
	for _, size := range [][2]int{{1, 4}, {10, 4}, {40, 10}, {130, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for range 3 {
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) != size[1] {
				t.Fatalf("%v: %d lines", size, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != size[0] {
					t.Fatalf("%v: line %d is %d cells: %q", size, i, w, ansi.Strip(l))
				}
			}
			key(m, tea.KeyPgDown, 0)
		}
	}
}

func TestHelpNarrowLayoutAndResize(t *testing.T) {
	m := keysModel(t, "key.down = ctrl+j\n")
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 12})
	key(m, tea.KeyF1, 0)
	v := ansi.Strip(m.View().Content)
	if !strings.Contains(v, "ctrl+j") || !strings.Contains(v, "Move down the list.") || !strings.Contains(v, "1–9/") {
		t.Fatalf("narrow help loses information:\n%s", v)
	}
	typeText(m, "G")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 100})
	if m.help.scroll != 0 || !strings.Contains(ansi.Strip(m.View().Content), "Move down the list.") {
		t.Fatalf("help after resize: scroll = %d", m.help.scroll)
	}
}
