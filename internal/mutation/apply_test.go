package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// tree makes files with their content as their own name, so a test can
// tell after renames which file ended where.
func tree(t testing.TB, names ...string) string {
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

func content(t testing.TB, path string) string {
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
// hidden file left out of the buffer) is refused, and nothing is done.
func TestApplyNeverReplaces(t *testing.T) {
	dir := tree(t, "a", "x", ".hidden")
	p := Plan{
		{Kind: OpRename, From: "x", To: "y"},
		{Kind: OpRename, From: "a", To: ".hidden"},
	}
	done, err := Apply(dir, p, nil)
	if done != 0 || !errors.Is(err, ErrExists) {
		t.Fatalf("done %d, err %v; want nothing done and ErrExists", done, err)
	}
	if got := ls(t, dir); !slices.Equal(got, []string{".hidden", "a", "x"}) {
		t.Errorf("folder holds %q, want it untouched", got)
	}
}

// TestApplyRollsBack: a rename that fails in the second phase, past the
// check (here a folder that may not be written), sends the items still
// under temporary names back to their own: y is done, a is back, and no
// ".fndr-rename-" name is left.
func TestApplyRollsBack(t *testing.T) {
	dir := tree(t, "a", "x", "ro/keep")
	ro := filepath.Join(dir, "ro")
	if err := os.Chmod(ro, 0o500); err != nil { //nolint:gosec // read-only on purpose
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) }) //nolint:errcheck,gosec // so TempDir can remove it
	p := Plan{
		{Kind: OpRename, From: "x", To: "y"},
		{Kind: OpRename, From: "a", To: "ro/a"},
	}
	done, err := Apply(dir, p, nil)
	if err == nil {
		t.Skip("a read-only folder took a file: running as root?")
	}
	if done != 1 {
		t.Errorf("done %d, want 1", done)
	}
	if got := ls(t, dir); !slices.Equal(got, []string{"a", "ro", "y"}) {
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

// TestApplyChecksFirst: a new name that is taken on disk, though Diff
// could not see it, is found before anything is done. The plan starts with
// a delete, so a check made only on the way would leave it half done.
func TestApplyChecksFirst(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files []string
		op    Op
		want  error
	}{
		{"a hidden name", []string{".env"}, Op{Kind: OpRename, From: "x", To: ".env"}, ErrExists},
		{"a create", []string{".env"}, Op{Kind: OpCreate, To: ".env"}, ErrExists},
		{"deep", []string{"a/b"}, Op{Kind: OpRename, From: "x", To: "a/b"}, ErrExists},
		{"a file on the way", []string{"notes"}, Op{Kind: OpRename, From: "x", To: "notes/x"}, ErrNotFolder},
		{"a file on the way to a create", []string{"notes"}, Op{Kind: OpCreate, To: "notes/a/"}, ErrNotFolder},
	} {
		dir := tree(t, append(tt.files, "old", "x")...)
		done, err := Apply(dir, Plan{{Kind: OpDelete, From: "old"}, tt.op}, freedesktop(t.TempDir()))
		if done != 0 || !errors.Is(err, tt.want) {
			t.Errorf("%s: done %d, err %v; want nothing done and %v", tt.name, done, err, tt.want)
		}
		if content(t, filepath.Join(dir, "old")) != "old" {
			t.Errorf("%s: old went to the trash before the check", tt.name)
		}
	}
}

// TestApplyCheckSeesWhatLeaves: a name held by something the same plan
// moves away is free. The check must not refuse what two phases allow.
func TestApplyCheckSeesWhatLeaves(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files []string
		plan  Plan
	}{
		{"trashed, then taken", []string{"a/b"}, Plan{{Kind: OpDelete, From: "a"}, {Kind: OpCreate, To: "a/b"}}},
		{"moved, then taken", []string{"a/b", "x"}, Plan{{Kind: OpRename, From: "a", To: "c"}, {Kind: OpRename, From: "x", To: "a/b"}}},
		{"a new case", []string{"readme"}, Plan{{Kind: OpRename, From: "readme", To: "README"}}},
		{"nested from", []string{"src/x"}, Plan{{Kind: OpRename, From: "src/x", To: "src/y"}}},
	} {
		dir := tree(t, tt.files...)
		if done, err := Apply(dir, tt.plan, freedesktop(t.TempDir())); err != nil || done != len(tt.plan) {
			t.Errorf("%s: done %d, err %v", tt.name, done, err)
		}
	}
}

// TestApplyCaseInsensitive: where the file system takes "A.txt" for
// "a.txt" (macOS by default), creating one beside the other is refused
// before anything is done.
func TestApplyCaseInsensitive(t *testing.T) {
	dir := tree(t, "a.txt", "old")
	if _, err := os.Lstat(filepath.Join(dir, "A.txt")); err != nil {
		t.Skip("this file system tells A.txt from a.txt")
	}
	done, err := Apply(dir, Plan{{Kind: OpDelete, From: "old"}, {Kind: OpCreate, To: "A.txt"}}, freedesktop(t.TempDir()))
	if done != 0 || !errors.Is(err, ErrExists) || content(t, filepath.Join(dir, "old")) != "old" {
		t.Errorf("done %d, err %v; want nothing done and ErrExists", done, err)
	}
}

// BenchmarkCheck: renaming a whole folder of 1000 files in a ring (f0 to
// f1, f1 to f2, ...), the worst case for check: every new name is taken
// and found among the leaving ones only by comparing.
func BenchmarkCheck(b *testing.B) {
	const n = 1000
	names := make([]string, n)
	p := make(Plan, n)
	for i := range n {
		names[i] = "f" + strconv.Itoa(i)
		p[i] = Op{Kind: OpRename, From: "f" + strconv.Itoa(i), To: "f" + strconv.Itoa((i+1)%n)}
	}
	dir := tree(b, names...)
	for b.Loop() {
		if err := check(dir, p); err != nil {
			b.Fatal(err)
		}
	}
}

// TestUndoNamesWhatItLeaves: an item that cannot go back to its old name
// (something took it meanwhile) stays under its temporary one, which is
// hidden, so the error must say where it is: a file the user cannot see is
// a file lost.
func TestUndoNamesWhatItLeaves(t *testing.T) {
	dir := tree(t, ".fndr-rename-1-0", ".fndr-rename-1-1", "b")
	temps := []string{filepath.Join(dir, ".fndr-rename-1-0"), filepath.Join(dir, ".fndr-rename-1-1")}
	err := undo(dir, []Op{{From: "a"}, {From: "b"}}, temps) // b is taken
	if err == nil || !strings.Contains(err.Error(), `"b" is left as ".fndr-rename-1-1"`) {
		t.Errorf("err %v; want it to name where b is", err)
	}
	if got := ls(t, dir); !slices.Equal(got, []string{".fndr-rename-1-1", "a", "b"}) {
		t.Errorf("folder holds %q: want a back, b's temporary name kept", got)
	}
}

// TestApplyMoveAway: a whole path moves an entry to another folder, made
// on the way; a name taken there stops the plan before any step.
func TestApplyMoveAway(t *testing.T) {
	home := t.TempDir()
	dl := filepath.Join(home, "Downloads")
	writeTestFile(t, filepath.Join(dl, "Kafka", "lesson1.mp4"))
	to := filepath.Join(home, "Documents", "video", "Kafka")
	plan, err := Diff([]Item{{1, "Kafka/"}}, []Line{{1, to + "/"}})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := Apply(dl, plan, nil); err != nil || n != 1 {
		t.Fatalf("Apply: %d, %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(to, "lesson1.mp4")); err != nil {
		t.Errorf("not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dl, "Kafka")); err == nil {
		t.Error("still in Downloads")
	}

	writeTestFile(t, filepath.Join(dl, "Kafka", "x"))
	if n, err := Apply(dl, plan, nil); !errors.Is(err, ErrExists) || n != 0 {
		t.Errorf("onto a taken name: %d, %v; want ErrExists before any step", n, err)
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestApplyMoveAwayCheckedFirst: a move away onto a taken name is found
// before any step, so a delete in the same plan has not happened yet.
func TestApplyMoveAwayCheckedFirst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	dl := filepath.Join(home, "Downloads")
	writeTestFile(t, filepath.Join(dl, "Kafka", "a"))
	writeTestFile(t, filepath.Join(dl, "junk.txt"))
	to := filepath.Join(home, "Documents", "Kafka")
	writeTestFile(t, filepath.Join(to, "b")) // taken
	plan, err := Diff([]Item{{1, "Kafka/"}, {2, "junk.txt"}}, []Line{{1, to + "/"}})
	if err != nil {
		t.Fatal(err)
	}
	trash, err := HomeTrash()
	if err != nil {
		t.Fatal(err)
	}
	if n, err := Apply(dl, plan, trash); !errors.Is(err, ErrExists) || n != 0 {
		t.Errorf("%d, %v; want nothing done and ErrExists", n, err)
	}
	if _, err := os.Stat(filepath.Join(dl, "junk.txt")); err != nil {
		t.Error("junk.txt was trashed before the move failed")
	}
}
