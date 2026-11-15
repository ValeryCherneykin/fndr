package mutation

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
	p := mustPlan(t, []Item{{ID: 1, Name: "notes.md"}}, []Line{{1, "notes.md"}, {1, "notes copy.md"}})
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
	p := mustPlan(t, []Item{{ID: 1, Name: "src/"}}, []Line{{1, "src/"}, {1, "lib/"}})
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
	items := []Item{{ID: 1, Name: "a.txt"}, {ID: 2, Name: "b.txt"}, {ID: 3, Name: "junk.txt"}, {ID: 4, Name: "src/"}}
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
		p := mustPlan(t, []Item{{ID: 1, Name: "src/"}}, []Line{{1, "src/"}, {1, "lib/"}})
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
	p := mustPlan(t, []Item{{ID: 1, Name: "main.go"}}, []Line{{1, "a.go"}, {1, "b.go"}})
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
	p := mustPlan(t, []Item{{ID: 1, Name: "a.txt"}, {ID: 2, Name: "b.txt"}, {ID: 3, Name: "junk.txt"}},
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

// TestDiffAway: an item yanked in another folder is copied in by each of
// its lines, under the name written there, and left alone with none.
func TestDiffAway(t *testing.T) {
	items := []Item{{ID: 1, Name: "main.go"}, {ID: 2, Name: "/home/me/a/notes.md", Away: true}}
	cases := []struct {
		name  string
		lines []Line
		want  Plan
	}{
		{"not put", []Line{{1, "main.go"}}, nil},
		{"put", []Line{{1, "main.go"}, {2, "notes.md"}},
			Plan{{Kind: OpCopy, From: "/home/me/a/notes.md", To: "notes.md"}}},
		{"put first", []Line{{2, "notes.md"}, {1, "main.go"}},
			Plan{{Kind: OpCopy, From: "/home/me/a/notes.md", To: "notes.md"}}},
		{"renamed, twice", []Line{{1, "main.go"}, {2, "a.md"}, {2, "b/c.md"}}, Plan{
			{Kind: OpCopy, From: "/home/me/a/notes.md", To: "a.md"},
			{Kind: OpCopy, From: "/home/me/a/notes.md", To: "b/c.md"}}},
	}
	for _, c := range cases {
		p, err := Diff(items, c.lines)
		if err != nil || !slices.Equal(p, c.want) {
			t.Errorf("%s: %v, %v; want %v", c.name, p, err, c.want)
		}
	}
}

// TestCopyFromAway: what was yanked in one folder is copied into another,
// and stays where it was.
func TestCopyFromAway(t *testing.T) {
	from, to := tree(t, "a/notes.md", "a/src/x.go"), tree(t, "main.go")
	items := []Item{{ID: 1, Name: "main.go"},
		{ID: 2, Name: filepath.Join(from, "a", "notes.md"), Away: true},
		{ID: 3, Name: filepath.Join(from, "a", "src") + "/", Away: true}}
	p := mustPlan(t, items, []Line{{1, "main.go"}, {2, "notes.md"}, {3, "src/"}})
	if n, err := Apply(to, p, nil); err != nil || n != 2 {
		t.Fatalf("Apply: %d, %v", n, err)
	}
	if content(t, filepath.Join(to, "notes.md")) != "a/notes.md" || content(t, filepath.Join(to, "src", "x.go")) != "a/src/x.go" {
		t.Error("the copies do not hold the bytes")
	}
	if content(t, filepath.Join(from, "a", "notes.md")) != "a/notes.md" {
		t.Error("the original left")
	}
}

// TestCopyFromAwayRefusedFirst: a copy whose source is gone, or lies in a
// folder the same plan trashes, stops the plan before anything is done.
func TestCopyFromAwayRefusedFirst(t *testing.T) {
	dir := tree(t, "old/notes.md", "keep.txt")
	gone := []Item{{ID: 1, Name: "keep.txt"}, {ID: 2, Name: filepath.Join(dir, "nowhere.md"), Away: true}}
	p := mustPlan(t, gone, []Line{{1, "keep.txt"}, {2, "x.md"}})
	if _, err := Apply(dir, p, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("source gone: %v, want ErrNotExist", err)
	}

	trashed := []Item{{ID: 1, Name: "old/"}, {ID: 2, Name: filepath.Join(dir, "old", "notes.md"), Away: true}}
	p = mustPlan(t, trashed, []Line{{2, "notes.md"}}) // dd old/, p notes.md
	trash := &Trash{files: t.TempDir()}
	if n, err := Apply(dir, p, trash); !errors.Is(err, ErrNested) || n != 0 {
		t.Errorf("source trashed: %d, %v; want 0, ErrNested", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old", "notes.md")); err != nil {
		t.Error("the folder went to the trash anyway")
	}
}

// dyingContext is done from its n-th look on: a copy stopped at an exact
// point, without timing.
type dyingContext struct {
	context.Context
	looks, n int
}

func (c *dyingContext) Err() error {
	c.looks++
	if c.looks >= c.n {
		return context.Canceled
	}
	return nil
}

// TestCopyProgress: Progress counts every entry copied and every byte,
// a file bigger than a chunk included.
func TestCopyProgress(t *testing.T) {
	dir := tree(t, "src/a.txt", "src/deep/b.txt")
	big := bytes.Repeat([]byte("x"), 2*copyChunk+5)
	if err := os.WriteFile(filepath.Join(dir, "src", "big.bin"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	p := mustPlan(t, []Item{{ID: 1, Name: "src/"}}, []Line{{1, "src/"}, {1, "lib/"}})
	var prog Progress
	if _, err := ApplyContext(context.Background(), dir, p, nil, &prog); err != nil {
		t.Fatal(err)
	}
	wantBytes := int64(len(big) + len("src/a.txt") + len("src/deep/b.txt"))
	// a.txt, big.bin, b.txt, deep/, and src/ itself.
	if e, b := prog.Entries.Load(), prog.Bytes.Load(); e != 5 || b != wantBytes {
		t.Errorf("progress: %d entries, %d bytes; want 5, %d", e, b, wantBytes)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "lib", "big.bin")); !bytes.Equal(got, big) { //nolint:errcheck // compared below
		t.Error("the big file was not copied whole")
	}
}

// TestCopyStopped: a copy stopped at any point leaves nothing of itself,
// the original whole, and says it was stopped; stopped before the first
// step, nothing is done at all.
func TestCopyStopped(t *testing.T) {
	for n := 1; n <= 12; n++ {
		dir := tree(t, "src/a.txt", "src/deep/b.txt", "src/deep/c.txt", "junk.txt")
		if err := os.WriteFile(filepath.Join(dir, "src", "big.bin"), bytes.Repeat([]byte("x"), 2*copyChunk), 0o600); err != nil {
			t.Fatal(err)
		}
		p := mustPlan(t, []Item{{ID: 1, Name: "src/"}, {ID: 2, Name: "junk.txt"}}, []Line{{1, "src/"}, {1, "lib/"}, {2, "junk.txt"}})
		ctx := &dyingContext{Context: context.Background(), n: n}
		_, err := ApplyContext(ctx, dir, p, nil, nil)
		if ctx.looks < n {
			continue // finished before it died: nothing to stop
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("n=%d: %v, want Canceled", n, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "lib")); err == nil || len(leftovers(t, dir)) > 0 {
			t.Errorf("n=%d: a part of the copy is left: %v", n, leftovers(t, dir))
		}
		if content(t, filepath.Join(dir, "src", "deep", "c.txt")) != "src/deep/c.txt" {
			t.Errorf("n=%d: the original was touched", n)
		}
	}
}

// TestStopPoints: each place a copy looks at its context is the one that
// stops it somewhere: before the first step, between entries of a tree of
// folders only, and inside one file.
func TestStopPoints(t *testing.T) {
	t.Run("before the first step", func(t *testing.T) {
		dir := tree(t, "a.txt")
		p := mustPlan(t, []Item{{ID: 1, Name: "a.txt"}}, []Line{{1, "b.txt"}})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if n, err := ApplyContext(ctx, dir, p, nil, nil); n != 0 || !errors.Is(err, context.Canceled) {
			t.Errorf("%d, %v; want 0, Canceled", n, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "a.txt")); err != nil {
			t.Error("renamed though stopped before")
		}
	})
	t.Run("between folders", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "src", "a", "b", "c"), 0o750); err != nil {
			t.Fatal(err)
		}
		p := mustPlan(t, []Item{{ID: 1, Name: "src/"}}, []Line{{1, "src/"}, {1, "lib/"}})
		ctx := &dyingContext{Context: context.Background(), n: 3}
		if _, err := ApplyContext(ctx, dir, p, nil, nil); !errors.Is(err, context.Canceled) {
			t.Errorf("%v, want Canceled", err)
		}
	})
	t.Run("inside a file", func(t *testing.T) {
		dir := t.TempDir()
		size := 3 * copyChunk
		if err := os.WriteFile(filepath.Join(dir, "big.bin"), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		p := mustPlan(t, []Item{{ID: 1, Name: "big.bin"}}, []Line{{1, "big.bin"}, {1, "copy.bin"}})
		ctx := &dyingContext{Context: context.Background(), n: 4} // ApplyContext, the file, a chunk, the next
		var prog Progress
		if _, err := ApplyContext(ctx, dir, p, nil, &prog); !errors.Is(err, context.Canceled) {
			t.Errorf("%v, want Canceled", err)
		}
		if b := prog.Bytes.Load(); b == 0 || b >= int64(size) {
			t.Errorf("stopped after %d bytes of %d: not inside the file", b, size)
		}
	})
}

// TestCopyStoppedReadOnly: a folder holding a read-only one (0500 here; 0555, as Go's
// module cache has), stopped at every point, leaves nothing behind: the
// half copy's read-only folder is emptied too.
func TestCopyStoppedReadOnly(t *testing.T) {
	for n := 1; n <= 10; n++ {
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		ro := filepath.Join(src, "a", "ro")
		if err := os.MkdirAll(ro, 0o750); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{filepath.Join(ro, "f"), filepath.Join(src, "z")} {
			if err := os.WriteFile(f, []byte("x"), 0o444); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chmod(ro, 0o500); err != nil { //nolint:gosec // read-only on purpose: what is tested
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o700) }) //nolint:gosec // for TempDir to remove it
		err := copyEntry(&dyingContext{Context: context.Background(), n: n}, src, filepath.Join(dir, "dst"), nil)
		names := ls(t, dir)
		switch {
		case err == nil && slices.Equal(names, []string{"dst", "src"}):
			_ = filepath.WalkDir(filepath.Join(dir, "dst"), func(p string, d fs.DirEntry, _ error) error {
				if d != nil && d.IsDir() {
					_ = os.Chmod(p, 0o700) //nolint:gosec // for TempDir to remove it
				}
				return nil
			})
		case err != nil && slices.Equal(names, []string{"src"}):
		default:
			t.Errorf("stopped at look %d: %v, left %q", n, err, names)
		}
	}
}

// TestCopySourceSwapped: a file swapped after it was looked at, for a
// named pipe or another file, is refused at once: open on a pipe would
// wait for a writer for good, and esc could not stop it.
func TestCopySourceSwapped(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(src)
	if err != nil {
		t.Fatal(err)
	}
	swap := map[string]func() error{
		"pipe":    func() error { return syscall.Mkfifo(src, 0o600) },
		"another": func() error { return os.WriteFile(src, []byte("y"), 0o600) },
	}
	for name, swapTo := range swap {
		if err := os.Remove(src); err != nil {
			t.Fatal(err)
		}
		if err := swapTo(); err != nil {
			t.Fatal(err)
		}
		done := async(func() error {
			f, err := openSource(src, info)
			if err == nil {
				_ = f.Close()
			}
			return err
		})
		select {
		case err := <-done:
			if !errors.Is(err, ErrNotCopied) {
				t.Errorf("%s: %v, want ErrNotCopied", name, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: open waits", name)
		}
	}
}

// async runs f in a goroutine: a test waits for it with a deadline.
func async(f func() error) <-chan error {
	c := make(chan error, 1)
	go func() { c <- f() }()
	return c
}
