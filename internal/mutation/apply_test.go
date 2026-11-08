package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tree makes files with their content as their own name, so a test can
// tell after renames which file ended where.
func tree(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func content(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func ls(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(des))
	for _, d := range des {
		names = append(names, d.Name())
	}
	return names
}

func TestApply(t *testing.T) {
	dir := tree(t, "a", "b", "old")
	trash := freedesktop(t.TempDir())
	p := Plan{
		{Kind: OpDelete, From: "old"},
		{Kind: OpRename, From: "a", To: "b"}, // a swap: only works in two phases
		{Kind: OpRename, From: "b", To: "a"},
		{Kind: OpCreate, To: "bin/main.txt"},
	}
	done, err := Apply(dir, p, trash)
	if err != nil || done != 4 {
		t.Fatalf("done %d, err %v", done, err)
	}
	if content(t, filepath.Join(dir, "a")) != "b" || content(t, filepath.Join(dir, "b")) != "a" {
		t.Error("a and b were not swapped")
	}
	if got := ls(t, dir); !slices.Equal(got, []string{"a", "b", "bin"}) {
		t.Errorf("folder holds %q", got)
	}
	if content(t, filepath.Join(trash.files, "old")) != "old" {
		t.Error("old is not in the trash")
	}
}

func TestApplyMovesIntoNewFolder(t *testing.T) {
	dir := tree(t, "main.go")
	if _, err := Apply(dir, Plan{{Kind: OpRename, From: "main.go", To: "cmd/app/main.go"}}, nil); err != nil {
		t.Fatal(err)
	}
	if content(t, filepath.Join(dir, "cmd", "app", "main.go")) != "main.go" {
		t.Error("main.go was not moved")
	}
}

// TestApplyNeverReplaces: a rename onto a name Diff could not see (a
// hidden file left out of the buffer) fails, and the plan is rolled back:
// every file is where it was, under its own name.
func TestApplyNeverReplaces(t *testing.T) {
	dir := tree(t, "a", "x", ".hidden")
	p := Plan{
		{Kind: OpRename, From: "x", To: "y"},
		{Kind: OpRename, From: "a", To: ".hidden"},
	}
	_, err := Apply(dir, p, nil)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err %v, want ErrExists", err)
	}
	if content(t, filepath.Join(dir, ".hidden")) != ".hidden" || content(t, filepath.Join(dir, "a")) != "a" {
		t.Error("a file was replaced or lost")
	}
	if got := ls(t, dir); !slices.Equal(got, []string{".hidden", "a", "y"}) {
		t.Errorf("folder holds %q: want y done, a back, no temporary names", got)
	}
}

func TestTrashKeepsBoth(t *testing.T) {
	trash := freedesktop(t.TempDir())
	for range 3 {
		dir := tree(t, "notes.md")
		if _, err := trash.Put(filepath.Join(dir, "notes.md")); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"notes 2.md", "notes 3.md", "notes.md"}
	if got := ls(t, trash.files); !slices.Equal(got, want) {
		t.Errorf("trash holds %q, want %q", got, want)
	}
	info := content(t, filepath.Join(trash.info, "notes 2.md.trashinfo"))
	if !strings.HasPrefix(info, "[Trash Info]\n") || !strings.Contains(info, "\nPath=/") {
		t.Errorf("info file %q", info)
	}
}

// TestTrashMac: the macOS trash has no info files.
func TestTrashMac(t *testing.T) {
	trash := &Trash{files: t.TempDir()}
	dir := tree(t, "d/f")
	if _, err := trash.Put(filepath.Join(dir, "d")); err != nil {
		t.Fatal(err)
	}
	if content(t, filepath.Join(trash.files, "d", "f")) != "d/f" {
		t.Error("the folder did not arrive whole")
	}
}
