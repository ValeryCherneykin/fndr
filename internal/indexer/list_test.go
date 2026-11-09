package indexer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestList(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"b.txt", "A.txt", ".zshrc", "c/x", "B-dir/x", ".git/HEAD", "a-dir/x"} {
		writeFile(t, filepath.Join(dir, f), "")
	}

	es, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(es))
	for i, e := range es {
		got[i] = e.Name()
	}
	want := []string{
		"a-dir", "B-dir", "c", // folders, ignoring case
		"A.txt", "b.txt", // then files
		".git", ".zshrc", // hidden last, folders first again
	}
	if !slices.Equal(got, want) {
		t.Errorf("List order = %q,\nwant          %q", got, want)
	}
	if !es[0].IsDir || es[3].IsDir {
		t.Errorf("IsDir wrong: %+v", es)
	}
	if es[0].Path != filepath.Join(dir, "a-dir") {
		t.Errorf("Path = %q, want an absolute path inside dir", es[0].Path)
	}
}

func TestListMissingDir(t *testing.T) {
	_, err := List(filepath.Join(t.TempDir(), "nope"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want ErrNotExist", err)
	}
}

func TestListEmptyDir(t *testing.T) {
	es, err := List(t.TempDir())
	if err != nil || len(es) != 0 {
		t.Errorf("List(empty) = %v, %v; want nothing", es, err)
	}
}

// TestListLinks: a link to a folder is listed as a folder, to be entered;
// a link to a file, or a broken one, as a file.
func TestListLinks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real", "x"), "")
	writeFile(t, filepath.Join(dir, "file"), "")
	for link, target := range map[string]string{"todir": "real", "tofile": "file", "broken": "nowhere"} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	es, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	isDir := map[string]bool{}
	for _, e := range es {
		isDir[e.Name()] = e.IsDir
	}
	if !isDir["todir"] || isDir["tofile"] || isDir["broken"] {
		t.Errorf("IsDir by name: %v", isDir)
	}
}

// BenchmarkList lists a folder of 1000 files and 20 links, about what a
// busy folder holds. Links cost an extra stat each (see List).
func BenchmarkList(b *testing.B) {
	dir := b.TempDir()
	for i := range 1000 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%04d.go", i)), nil, 0o600); err != nil {
			b.Fatal(err)
		}
	}
	for i := range 20 {
		if err := os.Symlink(fmt.Sprintf("file%04d.go", i), filepath.Join(dir, fmt.Sprintf("link%02d", i))); err != nil {
			b.Fatal(err)
		}
	}
	for b.Loop() {
		if _, err := List(dir); err != nil {
			b.Fatal(err)
		}
	}
}
