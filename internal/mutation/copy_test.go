package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func mustPlan(t *testing.T, items []Item, lines []Line) Plan {
	t.Helper()
	p, err := Diff(items, lines)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// leftovers lists the temporary copies left in dir: there must be none.
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error { //nolint:errcheck // a test listing
		if d != nil && strings.HasPrefix(d.Name(), ".fndr-copy-") {
			found = append(found, p)
		}
		return nil
	})
	return found
}

// TestCopyFile: a file is copied with its bytes, mode and time; the
// original stays.
func TestCopyFile(t *testing.T) {
	dir := tree(t, "notes.md")
	src := filepath.Join(dir, "notes.md")
	old := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	if err := os.Chmod(src, 0o640); err != nil { //nolint:gosec // a mode to keep
		t.Fatal(err)
	}
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	p := mustPlan(t, []Item{{1, "notes.md"}}, []Line{{1, "notes.md"}, {1, "notes copy.md"}})
	if n, err := Apply(dir, p, nil); err != nil || n != 1 {
		t.Fatalf("Apply: %d, %v", n, err)
	}
	cp := filepath.Join(dir, "notes copy.md")
	if content(t, cp) != "notes.md" || content(t, src) != "notes.md" {
		t.Error("the copy or the original does not hold the bytes")
	}
	info, err := os.Stat(cp)
	if err != nil || info.Mode().Perm() != 0o640 || !info.ModTime().Equal(old) {
		t.Errorf("copy: mode %v, time %v, %v; want 0640 and %v", info.Mode().Perm(), info.ModTime(), err, old)
	}
}

// TestCopyFolder: a folder is copied with all it holds; a link inside
// stays a link, never what it points to.
func TestCopyFolder(t *testing.T) {
	dir := tree(t, "src/main.go", "src/pkg/util.go")
	if err := os.Symlink("pkg/util.go", filepath.Join(dir, "src", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "src", "pkg"), 0o750); err != nil { //nolint:gosec // a mode to keep
		t.Fatal(err)
	}
	p := mustPlan(t, []Item{{1, "src/"}}, []Line{{1, "src/"}, {1, "lib/"}})
	if _, err := Apply(dir, p, nil); err != nil {
		t.Fatal(err)
	}
	if content(t, filepath.Join(dir, "lib", "pkg", "util.go")) != "src/pkg/util.go" {
		t.Error("the folder's insides were not copied")
	}
	if target, err := os.Readlink(filepath.Join(dir, "lib", "link")); err != nil || target != "pkg/util.go" {
		t.Errorf("the link: %q, %v; want a link to pkg/util.go", target, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "lib", "pkg")); err != nil || info.Mode().Perm() != 0o750 {
		t.Errorf("a folder's mode: %v, %v; want 0750 kept", info.Mode().Perm(), err)
	}
	if got := leftovers(t, dir); len(got) != 0 {
		t.Errorf("temporary copies left: %q", got)
	}
}

// TestCopyRefusedFirst: a copy that cannot be made is refused before any
// step: onto a taken name, or a folder into itself (which would read
// what it writes, until the disk is full).
func TestCopyRefusedFirst(t *testing.T) {
	// .hidden is on the disk but not in the buffer, as a hidden name is
	// when hidden names are not shown.
	dir := tree(t, "a.txt", "b.txt", "junk.txt", ".hidden", "src/x.go")
	items := []Item{{1, "a.txt"}, {2, "b.txt"}, {3, "junk.txt"}, {4, "src/"}}
	all := func(more ...Line) []Line {
		return append([]Line{{1, "a.txt"}, {2, "b.txt"}, {3, "junk.txt"}, {4, "src/"}}, more...)
	}
	for _, tc := range []struct {
		name  string
		lines []Line
		want  error
	}{
		{"onto a name in the buffer", all(Line{1, "b.txt"}), ErrConflict},
		{"onto a name taken on the disk", all(Line{1, ".hidden"}), ErrExists},
		{"into itself", all(Line{4, "src/deeper/"}), ErrIntoItself},
	} {
		p, err := Diff(items, tc.lines)
		if err == nil {
			_, err = Apply(dir, p, nil)
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
		if _, err := os.Stat(filepath.Join(dir, "junk.txt")); err != nil {
			t.Fatalf("%s: a step ran before the refusal: junk.txt is gone", tc.name)
		}
	}
}

// TestCopyStoppedLeavesNothing: a copy stopped halfway (a file it cannot
// read, a pipe it will not) leaves no half copy, under its name or any.
func TestCopyStoppedLeavesNothing(t *testing.T) {
	dir := tree(t, "src/a.go", "src/b.go")
	locked := filepath.Join(dir, "src", "b.go")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) }) //nolint:errcheck,gosec // so TempDir can remove it
	if err := syscall.Mkfifo(filepath.Join(dir, "src", "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, unreadable := range []bool{true, false} {
		if !unreadable {
			_ = os.Chmod(locked, 0o600) //nolint:errcheck,gosec // now only the pipe stops it
		}
		p := mustPlan(t, []Item{{1, "src/"}}, []Line{{1, "src/"}, {1, "lib/"}})
		n, err := Apply(dir, p, nil)
		if err == nil || n != 0 {
			t.Fatalf("unreadable %v: %d, %v; want the copy refused", unreadable, n, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "lib")); err == nil {
			t.Error("half a copy under its name")
		}
		if got := leftovers(t, dir); len(got) != 0 {
			t.Errorf("temporary copies left: %q", got)
		}
	}
}

// TestCopyThenRename: an entry copied and renamed in one plan: the copy
// is of the entry as it was, made before it moves.
func TestCopyThenRename(t *testing.T) {
	dir := tree(t, "main.go")
	p := mustPlan(t, []Item{{1, "main.go"}}, []Line{{1, "a.go"}, {1, "b.go"}})
	if _, err := Apply(dir, p, nil); err != nil {
		t.Fatal(err)
	}
	if content(t, filepath.Join(dir, "a.go")) != "main.go" || content(t, filepath.Join(dir, "b.go")) != "main.go" {
		t.Error("want a.go (the original, renamed) and b.go (its copy)")
	}
	if _, err := os.Stat(filepath.Join(dir, "main.go")); err == nil {
		t.Error("main.go is still there")
	}
}

// TestCopyOntoNameFreedLater: copies run before renames, so a name a
// rename frees is still taken when the copy is made; the plan is refused
// before any step, not stopped after the trash has run.
func TestCopyOntoNameFreedLater(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	dir := tree(t, "a.txt", "b.txt", "junk.txt")
	p := mustPlan(t, []Item{{1, "a.txt"}, {2, "b.txt"}, {3, "junk.txt"}},
		[]Line{{1, "c.txt"}, {2, "b.txt"}, {2, "a.txt"}}) // junk.txt trashed, a → c, b copied to a
	trash, err := HomeTrash()
	if err != nil {
		t.Fatal(err)
	}
	if n, err := Apply(dir, p, trash); !errors.Is(err, ErrExists) || n != 0 {
		t.Errorf("%d, %v; want ErrExists before any step", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "junk.txt")); err != nil {
		t.Error("junk.txt was trashed before the refusal")
	}
}
