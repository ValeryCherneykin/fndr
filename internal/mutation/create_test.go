package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateFile(t *testing.T) {
	dir := t.TempDir()
	c, err := Create(dir, "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "notes.md")
	if c.Path != want || c.Top != want {
		t.Errorf("created %+v, want %s", c, want)
	}
	if info, err := os.Stat(want); err != nil || info.IsDir() || info.Size() != 0 {
		t.Errorf("on disk: %v %v, want an empty file", info, err)
	}
}

func TestCreateFolder(t *testing.T) {
	dir := t.TempDir()
	c, err := Create(dir, "drafts/")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, "drafts")); err != nil || !info.IsDir() {
		t.Fatalf("drafts/ made %v %v, want a folder", info, err)
	}
	if c.Path != filepath.Join(dir, "drafts") {
		t.Errorf("Path %q, want it without the trailing slash", c.Path)
	}

	// Nested: missing parents are made too.
	if _, err := Create(dir, "x/y/z/"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, "x", "y", "z")); err != nil || !info.IsDir() {
		t.Errorf("x/y/z/ made %v %v, want nested folders", info, err)
	}
}

// TestCreateNested: "a/b/c.txt" makes the folders on the way, and Top is
// the folder "a", which is what the listing of dir can show and select.
func TestCreateNested(t *testing.T) {
	dir := t.TempDir()
	c, err := Create(dir, "a/b/c.txt")
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != filepath.Join(dir, "a", "b", "c.txt") || c.Top != filepath.Join(dir, "a") {
		t.Errorf("created %+v, want path a/b/c.txt with top a", c)
	}
	if _, err := os.Stat(c.Path); err != nil {
		t.Error(err)
	}
}

// TestCreateNeverOverwrites is the rule a file manager lives or dies by.
func TestCreateNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(path, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"keep.txt", "keep.txt/"} {
		if _, err := Create(dir, name); !errors.Is(err, ErrExists) {
			t.Errorf("Create(%q) over an existing file: %v, want ErrExists", name, err)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != "precious" {
		t.Errorf("file now holds %q: it was overwritten", b)
	}
}

func TestCreateRefusesBadNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "here")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	tests := map[string]error{
		"":            ErrEmptyName,
		"   ":         ErrEmptyName,
		"/":           ErrEmptyName,
		"../escape":   ErrOutside,
		"a/../../out": ErrOutside,
		"/etc/x":      ErrOutside,
		".":           ErrEmptyName, // names the folder itself: nothing new
		"..":          ErrOutside,
	}
	for name, want := range tests {
		if _, err := Create(dir, name); !errors.Is(err, want) {
			t.Errorf("Create(%q) = %v, want %v", name, err, want)
		}
	}
	if es, _ := os.ReadDir(filepath.Dir(dir)); len(es) != 1 {
		t.Errorf("something was created outside the folder: %v", es)
	}
}

// TestCreateDotsInsideAreFine: ".." that stays inside is just a path, and
// a name that merely starts with dots is a hidden file, not an escape.
func TestCreateDotsInsideAreFine(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a/../b.txt", "..hidden", ".env"} {
		if _, err := Create(dir, name); err != nil {
			t.Errorf("Create(%q): %v", name, err)
		}
	}
}
