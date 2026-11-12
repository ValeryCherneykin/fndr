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
preview = on
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
	lines := make([]int, 0, len(errs))
	for _, err := range errs {
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("%v is not an *Error", err)
		}
		lines = append(lines, e.Line)
	}
	if !slices.Equal(lines, []int{7, 8, 9, 10, 11, 12, 13}) {
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
	if c, errs := Load(filepath.Join(dir, "none")); len(c.Binds) != 0 || errs != nil {
		t.Errorf("no file: %+v, %v; want nothing, no error", c, errs)
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

// FuzzParse: any file at all, Parse never panics, and every key it keeps
// reads back as itself.
func FuzzParse(f *testing.F) {
	f.Add("bind = ctrl+f>f = tag:dirs\n# x\nbind=alt+x=tag:.go")
	f.Add("bind = > = tag:x")
	f.Fuzz(func(t *testing.T, s string) {
		c, _ := Parse(strings.NewReader(s))
		for _, b := range c.Binds {
			for _, k := range b.Keys {
				if again, err := Keystroke(k); err != nil || again != k {
					t.Fatalf("%q reads back as %q, %v", k, again, err)
				}
			}
		}
	})
}
