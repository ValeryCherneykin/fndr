package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSafeText(t *testing.T) {
	tests := map[string]string{
		"main.go":                 "main.go",
		"заметки.md":              "заметки.md",
		"日本.txt":                  "日本.txt",
		"a\nb":                    "a?b",
		"\x1b]52;c;aGk=\a":        "?]52;c;aGk=?",
		"\x1b[2J":                 "?[2J",
		"del\x7f":                 "del?",
		"c1\u009b31m":             "c1·31m",
		"bad\xffutf8":             "bad?utf8",
		"invoice\u202efdp.exe":    "invoice\ufffdfdp.exe",
		"tab\there":               "tab?here",
		"real \ufffd replacement": "real \ufffd replacement",
	}
	for in, want := range tests {
		got := safeText(in)
		if got != want {
			t.Errorf("safeText(%q) = %q, want %q", in, got, want)
		}
		if len(got) != len(in) {
			t.Errorf("safeText(%q): %d bytes, want %d: offsets would shift", in, len(got), len(in))
		}
	}
}

// FuzzSafeText: whatever a name holds, the result is valid UTF-8 of the
// same length that a terminal cannot act on.
func FuzzSafeText(f *testing.F) {
	for _, s := range []string{"main.go", "\x1b[31m", "\u009b", "\xff\xfe", "\u202e"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := safeText(s)
		if len(got) != len(s) {
			t.Fatalf("length %d, want %d", len(got), len(s))
		}
		if !utf8.ValidString(got) {
			t.Fatalf("%q is not valid UTF-8", got)
		}
		for _, r := range got {
			if r < 0x20 || r == 0x7f || 0x80 <= r && r <= 0x9f || isBidi(r) {
				t.Fatalf("%q still holds %U", got, r)
			}
		}
	})
}

// TestHostileNameOnScreen is the real attack: a folder holding a file whose
// name is an escape sequence. Nothing of it may reach the terminal as a
// command, and the screen keeps its shape.
func TestHostileNameOnScreen(t *testing.T) {
	m, root := browseModel(t, "")
	evil := "\x1b]52;c;cHduZWQ=\a\x1b[2J\nboom"
	if err := os.WriteFile(filepath.Join(root, evil), nil, 0o600); err != nil {
		t.Skip("file system refuses the name:", err)
	}
	send(m, m.load(m.cwd)())
	m.selectName(t, evil)
	m.setNotice("opened "+evil, false)

	view := m.View().Content
	for _, bad := range []string{"\x1b]", "\x1b[2J", "\a", "\nboom"} {
		if strings.Contains(view, bad) {
			t.Errorf("screen carries %q", bad)
		}
	}
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Errorf("%d lines, want %d: the name broke the screen", len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.width {
			t.Errorf("line %d is %d cells, want %d", i, w, m.width)
		}
	}
}

func TestPasteDropsControls(t *testing.T) {
	m := testModel(t)
	send(m, tea.PasteMsg{Content: "main\x1b[31m\u202e.go\n"})
	if m.query != "main[31m.go" {
		t.Errorf("query %q", m.query)
	}
}
