package indexer

import (
	"errors"
	"io/fs"
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
