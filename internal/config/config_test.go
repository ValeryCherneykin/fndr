package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	c, errs := Parse(strings.NewReader(`
# tags a chord away
bind = ctrl+f>f = tag:dirs
  bind=Ctrl+F > T = tag: @today
bind = alt+shift+x = tag:has:.json|.yaml

bind ctrl+ff = tag:dirs
bind = ctrl+f>f
bind = ctrl+q = open:x
bind = ctrl+q = tag:
bind = ctrl+fx = tag:.go
bind = cmd+f = tag:.go
preview = maybe
bind = ctrl+g = tag:dirs   # folders
`))
	want := []Bind{
		{Keys: []string{"ctrl+f", "f"}, Action: "tag", Arg: "dirs", Line: 3},
		{Keys: []string{"ctrl+f", "t"}, Action: "tag", Arg: "@today", Line: 4},
		{Keys: []string{"alt+shift+x"}, Action: "tag", Arg: "has:.json|.yaml", Line: 5},
	}
	if len(c.Binds) != len(want) {
		t.Fatalf("binds %+v\nwant %+v", c.Binds, want)
	}
	for i := range want {
		g, w := c.Binds[i], want[i]
		if !slices.Equal(g.Keys, w.Keys) || g.Action != w.Action || g.Arg != w.Arg || g.Line != w.Line {
			t.Errorf("bind %d: %+v, want %+v", i, g, w)
		}
	}
	// Every bad line is reported, by number, and the good ones still count.
	if lines := errLines(t, errs); !slices.Equal(lines, []int{7, 8, 9, 10, 11, 12, 13, 14}) {
		t.Errorf("errors on lines %v: %v", lines, errs)
	}
}

func TestKeystroke(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl+f":           "ctrl+f",
		"Ctrl+F":           "ctrl+f",
		"shift+alt+ctrl+x": "ctrl+alt+shift+x", // the interface's order
		"f":                "f",
		"F5":               "f5",
		"f24":              "f24",
		"ctrl+pgup":        "ctrl+pgup",
		"super+ж":          "super+ж",
		"esc":              "esc",
	} {
		if got, err := Keystroke(in); err != nil || got != want {
			t.Errorf("Keystroke(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ctrl+", "ctrl++", "cmd+f", "ctrl+ctrl+f", "ctrl+ff", "f0", "f25", "f05", "ctrl+ ", "\x1b"} {
		if got, err := Keystroke(bad); err == nil {
			t.Errorf("Keystroke(%q) = %q, want an error", bad, got)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if c, errs := Load(filepath.Join(dir, "none")); len(c.Binds) != 0 || !c.Icons || len(c.Keys["down"]) == 0 || errs != nil {
		t.Errorf("no file: %+v, %v; want the defaults, no error", c, errs)
	}
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("bind = ctrl+f>f = tag:dirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, errs := Load(path); len(c.Binds) != 1 || errs != nil {
		t.Errorf("a file: %+v, %v", c, errs)
	}
	if _, errs := Load(dir); len(errs) != 1 {
		t.Errorf("a folder: %v, want one error", errs)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/conf")
	if p, err := DefaultPath(); err != nil || p != "/x/conf/fndr/config" {
		t.Errorf("with XDG_CONFIG_HOME: %q, %v", p, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative") // the spec says to ignore it
	t.Setenv("HOME", "/home/me")
	if p, err := DefaultPath(); err != nil || p != "/home/me/.config/fndr/config" {
		t.Errorf("without: %q, %v", p, err)
	}
}

// FuzzParse: any file at all, Parse never panics, every key it keeps reads
// back as itself, and no key does two actions.
func FuzzParse(f *testing.F) {
	f.Add("bind = ctrl+f>f = tag:dirs\n# x\nbind=alt+x=tag:.go")
	f.Add("bind = > = tag:x")
	f.Add("key.down = ctrl+j\nkey.up = ctrl+j ctrl+n\nkey.quit = none")
	f.Fuzz(func(t *testing.T, s string) {
		c, _ := Parse(strings.NewReader(s))
		owner := map[string]string{}
		for name, keys := range c.Keys {
			for _, k := range keys {
				if again, err := Keystroke(k); err != nil || again != k {
					t.Fatalf("%s: %q reads back as %q, %v", name, k, again, err)
				}
				if prev, ok := owner[k]; ok {
					t.Fatalf("%s does %s and %s", k, prev, name)
				}
				owner[k] = name
			}
		}
		for _, b := range c.Binds {
			for _, k := range b.Keys {
				if again, err := Keystroke(k); err != nil || again != k {
					t.Fatalf("%q reads back as %q, %v", k, again, err)
				}
			}
		}
	})
}

// TestParseByteOrderMark: a mark some editors put first is not a setting.
func TestParseByteOrderMark(t *testing.T) {
	c, errs := Parse(strings.NewReader("\ufeffbind = ctrl+f>f = tag:dirs\n"))
	if len(errs) != 0 || len(c.Binds) != 1 {
		t.Errorf("binds %+v, errors %v", c.Binds, errs)
	}
}

// TestDefault: a file that sets nothing is fndr as it comes; the table's
// keys are written as the interface names them, and none does two actions.
func TestDefault(t *testing.T) {
	c, errs := Parse(strings.NewReader("# nothing\n"))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if !c.Icons || c.Preview || !c.Images || !c.History || !c.SaveIndex || c.Editor != "" || c.EditorGUI {
		t.Errorf("defaults %+v", c)
	}
	owner := map[string]string{}
	for _, a := range Actions {
		if !slices.Equal(c.Keys[a.Name], a.Keys) {
			t.Errorf("%s: %q, want %q", a.Name, c.Keys[a.Name], a.Keys)
		}
		for _, k := range a.Keys {
			if again, err := Keystroke(k); err != nil || again != k {
				t.Errorf("%s: %q is written as %q, %v", a.Name, k, again, err)
			}
			if prev, ok := owner[k]; ok {
				t.Errorf("%s does %s and %s", k, prev, a.Name)
			}
			owner[k] = a.Name
		}
	}
	c.Keys["down"][0] = "x" // the table stays as it was
	if Default().Keys["down"][0] != "ctrl+n" {
		t.Error("Default hands out the table itself")
	}
}

// TestSettings: a switch is on or off (true, false, as Ghostty writes
// them), a text is taken as is, and every mistake names its line.
func TestSettings(t *testing.T) {
	c, errs := Parse(strings.NewReader(`images = off
preview = ON
history = false
editor = code --wait
editor_gui = true
icons = no
images = on
imagez = off
save_index = off # keep it
`))
	if c.Images || !c.Preview || c.History || c.Editor != "code --wait" || !c.EditorGUI || !c.Icons || !c.SaveIndex {
		t.Errorf("settings %+v", c)
	}
	if got := errLines(t, errs); !slices.Equal(got, []int{6, 7, 8, 9}) {
		t.Errorf("errors on lines %v: %v", got, errs)
	}
}

// TestKeys: an action's line is all its keys; "none" turns it off; a key
// written for one action is taken from the one that had it, which is said.
func TestKeys(t *testing.T) {
	c, errs := Parse(strings.NewReader(`key.down = ctrl+j Down
key.reveal = none
key.nope = ctrl+x
key.up = ctrl+x ctrl+x
key.up =
key.quit = ctrl+q
key.jump = ctrl+q
key.down = ctrl+n
`))
	for name, want := range map[string][]string{
		"down":         {"ctrl+j", "down"},
		"preview_down": {},               // ctrl+j went to down
		"reveal":       {},               // none
		"up":           {"ctrl+p", "up"}, // both lines wrong: the default
		"quit":         {},               // ctrl+q went to jump, a line below
		"jump":         {"ctrl+q"},
	} {
		if !slices.Equal(c.Keys[name], want) {
			t.Errorf("%s: %q, want %q", name, c.Keys[name], want)
		}
	}
	// 3 unknown, 4 a key twice, 5 no keys, 8 down twice; then what was
	// taken: ctrl+j by down (line 1), ctrl+q by jump (line 7).
	if got := errLines(t, errs); !slices.Equal(got, []int{3, 4, 5, 8, 1, 7}) {
		t.Errorf("errors on lines %v: %v", got, errs)
	}
}

// errLines returns the line of every error, which must all be *Error.
func errLines(t *testing.T, errs []error) []int {
	t.Helper()
	lines := make([]int, 0, len(errs))
	for _, err := range errs {
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("%v is not an *Error", err)
		}
		lines = append(lines, e.Line)
	}
	return lines
}

// TestEditorGUINeedsEditor: a window of its own is said of a named editor,
// never of $EDITOR, which would be started with no terminal.
func TestEditorGUINeedsEditor(t *testing.T) {
	c, errs := Parse(strings.NewReader("images = off\neditor_gui = on\n"))
	if c.EditorGUI || !slices.Equal(errLines(t, errs), []int{2}) {
		t.Errorf("gui %v, errors %v", c.EditorGUI, errs)
	}
	c, errs = Parse(strings.NewReader("editor_gui = on\neditor = code\n"))
	if !c.EditorGUI || len(errs) != 0 {
		t.Errorf("with an editor: gui %v, errors %v", c.EditorGUI, errs)
	}
}

// TestKeysThatType: a key that types is refused, where it would take a
// letter from the query; parent, which acts only on an empty query, may
// have one. backspace erases, whatever the file says.
func TestKeysThatType(t *testing.T) {
	c, errs := Parse(strings.NewReader(`key.down = j
key.up = shift+k
key.global = space
key.jump = backspace
key.parent = - shift+h
key.first = ctrl+j
`))
	if got := errLines(t, errs); !slices.Equal(got, []int{1, 2, 3, 4, 6}) {
		t.Errorf("errors on lines %v: %v", got, errs) // 6: ctrl+j taken from preview_down
	}
	if !slices.Equal(c.Keys["down"], []string{"ctrl+n", "down"}) || !slices.Equal(c.Keys["parent"], []string{"-", "shift+h"}) {
		t.Errorf("down %q, parent %q", c.Keys["down"], c.Keys["parent"])
	}
}
