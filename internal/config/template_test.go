package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestTemplateIsTheDefaults: the file written for a new user reads with no
// mistake as fndr with nothing set, and names every setting and action,
// each with a word on what it does.
func TestTemplateIsTheDefaults(t *testing.T) {
	tpl := Template()
	c, errs := Parse(strings.NewReader(tpl))
	if len(errs) != 0 {
		t.Fatalf("the template has mistakes: %v", errs)
	}
	if !reflect.DeepEqual(c, Default()) {
		t.Errorf("the template reads as\n%+v\nnot the defaults\n%+v", c, Default())
	}
	for _, s := range settings {
		last := s.doc[strings.LastIndexByte(s.doc, '\n')+1:]
		if s.doc == "" || !strings.Contains(tpl, "# "+last+"\n"+strings.TrimRight(s.name+" = "+s.def, " ")+"\n") {
			t.Errorf("%s: missing, or nothing said of it", s.name)
		}
	}
	for _, a := range Actions {
		if a.Doc == "" || !strings.Contains(tpl, "# "+a.Doc+"\nkey."+a.Name+" = ") {
			t.Errorf("key.%s: missing, or nothing said of it", a.Name)
		}
	}
	// The examples are what a bind is: taken out of the comment, they work.
	var binds []string
	for line := range strings.SplitSeq(tpl, "\n") {
		for _, key := range []string{"bind", "skip", "include"} {
			if b, ok := strings.CutPrefix(line, "# "+key+" = "); ok {
				binds = append(binds, key+" = "+b)
			}
		}
	}
	c, errs = Parse(strings.NewReader(strings.Join(binds, "\n")))
	if len(binds) == 0 || len(errs) != 0 || len(c.Binds)+len(c.Skip)+len(c.Include) != len(binds) {
		t.Errorf("examples %q: %v", binds, errs)
	}
}

// TestWriteNew: the template is written where there is no file, folders
// made; a file there, of the user's, is left byte for byte.
func TestWriteNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fndr", "config")
	if wrote, err := WriteNew(path); !wrote || err != nil {
		t.Fatalf("no file: wrote %v, %v", wrote, err)
	}
	if b, _ := os.ReadFile(path); string(b) != Template() {
		t.Error("written is not the template")
	}
	if err := os.WriteFile(path, []byte("bind = ctrl+f>f = tag:dirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if wrote, err := WriteNew(path); wrote || err != nil {
		t.Errorf("a file there: wrote %v, %v", wrote, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "bind = ctrl+f>f = tag:dirs\n" {
		t.Errorf("the user's file is now %q", b)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if wrote, err := WriteNew(filepath.Join(blocked, "config")); wrote || err == nil {
		t.Errorf("under a file: wrote %v, %v; want an error", wrote, err)
	}
}
