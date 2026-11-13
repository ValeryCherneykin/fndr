package indexer

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// crawlSorted crawls root, or refreshes it from saved, and returns the
// entries sorted, as a saved index holds them.
func crawlSorted(tb testing.TB, root string, prune *Prune, saved []Entry) ([]Entry, Stats) {
	tb.Helper()
	out := make(chan []Entry)
	var (
		stats Stats
		err   error
	)
	go func() {
		defer close(out)
		if saved == nil {
			stats, err = Crawl(context.Background(), root, prune, out)
		} else {
			stats, err = Refresh(context.Background(), root, prune, saved, out)
		}
	}()
	var all []Entry
	for b := range out {
		all = append(all, b...)
	}
	if err != nil {
		tb.Fatal(err)
	}
	return sortedCopy(all), stats
}

// names is entries in short, for failure messages.
func names(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = fmt.Sprintf("%s %t", e.Path, e.IsDir)
	}
	return out
}

// TestRefreshUnchanged: nothing changed, nothing read but the root: every
// other entered folder comes from the saved crawl.
func TestRefreshUnchanged(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"a/b/c.txt", "a/d.txt", "e/f/g/h.go", ".hidden/x", "node_modules/m/i.js", "bin/tool"} {
		writeFile(t, filepath.Join(root, f), "x")
	}
	writeFile(t, filepath.Join(root, ".gitignore"), "bin/\n")
	prune := DefaultPrune("")
	saved, _ := crawlSorted(t, root, prune, nil)
	got, stats := crawlSorted(t, root, prune, saved)
	if !slices.Equal(got, saved) {
		t.Errorf("refresh of an unchanged tree:\n got %v\nwant %v", names(got), names(saved))
	}
	// Entered: a, a/b, e, e/f, e/f/g. Not: the root (always read), and
	// .hidden, node_modules, bin, which are listed only.
	if stats.Reused != 5 {
		t.Errorf("%d folders reused, want 5", stats.Reused)
	}
}

// TestRefreshSeesWritesInPlace: a file saved over in place, as editors
// often do, leaves its folder's time as it was; a refresh must still see
// its new size and time, or @today misses this morning's edit.
func TestRefreshSeesWritesInPlace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "src", "main.go")
	writeFile(t, path, "package main")
	saved, _ := crawlSorted(t, root, nil, nil)

	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "package main\n\nfunc main() {}\n")
	later := dir.ModTime().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if now, err := os.Stat(filepath.Dir(path)); err != nil || !now.ModTime().Equal(dir.ModTime()) {
		t.Fatalf("the folder's time moved (%v): the test needs it not to", err)
	}

	got, stats := crawlSorted(t, root, nil, saved)
	if stats.Reused != 1 {
		t.Fatalf("%d folders reused, want src: the case is a folder that did not change", stats.Reused)
	}
	e, _ := find(got, path)
	if e.Size != 29 || e.ModTime != later.UnixNano() {
		t.Errorf("main.go after a refresh: size %d, time %v; want 29 and %v", e.Size, time.Unix(0, e.ModTime), later)
	}
}

// TestRefreshForgetsWhatIsGone: a file gone while its folder's time was
// set back (rsync, an archive unpacked) is left out, as a crawl would,
// not kept as a ghost that Enter cannot open.
func TestRefreshForgetsWhatIsGone(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	writeFile(t, filepath.Join(dir, "main.go"), "x")
	writeFile(t, filepath.Join(dir, "old.go"), "x")
	saved, _ := crawlSorted(t, root, nil, nil)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "old.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	got, stats := crawlSorted(t, root, nil, saved)
	if stats.Reused != 1 {
		t.Fatalf("%d folders reused, want src: the case is a folder that looks unchanged", stats.Reused)
	}
	if _, ok := find(got, filepath.Join(dir, "old.go")); ok {
		t.Error("old.go is gone but still indexed")
	}
}

// TestRefreshFindsWhatCrawlFinds: after random edits of every kind, a
// refresh finds exactly what a fresh crawl does, round after round, each
// refreshing the one before, as starts would.
func TestRefreshFindsWhatCrawlFinds(t *testing.T) {
	root := t.TempDir()
	prune := DefaultPrune("")
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // seeded on purpose: a failing round must replay
	dirs := make([]string, 1, 31)    // the root, then 30 folders
	for i := range 30 {
		d := filepath.Join(dirs[r.IntN(len(dirs))], fmt.Sprint("d", i))
		dirs = append(dirs, d)
		writeFile(t, filepath.Join(root, d, "f.txt"), "x")
	}
	saved, _ := crawlSorted(t, root, prune, nil)

	for round := range 40 {
		for range 3 {
			edit(t, r, root, dirs)
		}
		got, _ := crawlSorted(t, root, prune, saved)
		want, _ := crawlSorted(t, root, prune, nil)
		// Whole entries, sizes and times too: files are written in place
		// (edit, case 5), which no folder's time shows.
		if !slices.Equal(got, want) {
			t.Fatalf("round %d:\n got %v\nwant %v", round, names(got), names(want))
		}
		saved = got
	}
}

// edit changes the tree below root in one random way. Folders may be gone
// by the time they are picked: then nothing happens.
func edit(t *testing.T, r *rand.Rand, root string, dirs []string) {
	t.Helper()
	d := filepath.Join(root, dirs[r.IntN(len(dirs))])
	if _, err := os.Stat(d); err != nil {
		return
	}
	name := fmt.Sprint("n", r.IntN(1000))
	switch r.IntN(6) {
	case 0: // a new file
		try(filepath.Join(d, name), "x")
	case 1: // a new folder, with something in it
		try(filepath.Join(d, name, "inner", "y.txt"), "y")
	case 2: // something removed, folder or file
		if es, _ := os.ReadDir(d); len(es) > 0 { //nolint:errcheck // an unreadable folder is just not edited
			_ = os.RemoveAll(filepath.Join(d, es[r.IntN(len(es))].Name())) //nolint:errcheck // as above
		}
	case 3: // something renamed, folder or file
		if es, _ := os.ReadDir(d); len(es) > 0 { //nolint:errcheck // as above
			_ = os.Rename(filepath.Join(d, es[r.IntN(len(es))].Name()), filepath.Join(d, name)) //nolint:errcheck // as above
		}
	case 4: // a .gitignore that leaves out some folder, or stops leaving it out
		try(filepath.Join(d, ".gitignore"), strings.Repeat("x", r.IntN(3))+fmt.Sprint("d", r.IntN(30), "/\n"))
	case 5: // a file written in place: no name changes
		try(filepath.Join(d, "f.txt"), strings.Repeat("z", r.IntN(100)))
	}
}

// try writes a file, making folders on the way, and gives up quietly where
// a random edit collides with an earlier one (a file where a folder goes).
func try(path, content string) {
	if os.MkdirAll(filepath.Dir(path), 0o750) == nil {
		_ = os.WriteFile(path, []byte(content), 0o600) //nolint:errcheck,gosec // see above
	}
}

// BenchmarkRefreshHome compares a crawl of a real tree with a refresh of
// it when nothing changed: FNDR_BENCH_DIR=$HOME go test -bench RefreshHome.
func BenchmarkRefreshHome(b *testing.B) {
	root := os.Getenv("FNDR_BENCH_DIR")
	if root == "" {
		b.Skip("set FNDR_BENCH_DIR to a folder to index")
	}
	home, _ := os.UserHomeDir() //nolint:errcheck // without one, only the rules by name apply
	prune := DefaultPrune(home)
	saved, _ := crawlSorted(b, root, prune, nil)
	b.Run("crawl", func(b *testing.B) {
		for b.Loop() {
			crawlSorted(b, root, prune, nil)
		}
	})
	b.Run("refresh", func(b *testing.B) {
		var stats Stats
		for b.Loop() {
			_, stats = crawlSorted(b, root, prune, saved)
		}
		b.ReportMetric(float64(stats.Reused), "reused")
	})
}

// TestRefreshSeesGitignoreEdits: a .gitignore edited in place leaves its
// folder's time as it was, yet changes what is entered below. Folders it
// stops ignoring were listed but never entered: they must be crawled, not
// taken for empty. Folders it starts ignoring must lose their contents.
func TestRefreshSeesGitignoreEdits(t *testing.T) {
	root := t.TempDir()
	prune := DefaultPrune("")
	gi := filepath.Join(root, "a", ".gitignore")
	writeFile(t, gi, "b/\n")
	writeFile(t, filepath.Join(root, "a", "b", "x.txt"), "x")
	writeFile(t, filepath.Join(root, "a", "c", "y.txt"), "y")
	saved, _ := crawlSorted(t, root, prune, nil)

	for _, rules := range []string{"c/\n", "\n", "b/\nc/\n", "!keep\nb/\n", ""} {
		writeFile(t, gi, rules) // in place: a's time does not change
		got, _ := crawlSorted(t, root, prune, saved)
		want, _ := crawlSorted(t, root, prune, nil)
		if !slices.Equal(got, want) {
			t.Fatalf(".gitignore now %q:\n got %v\nwant %v", rules, names(got), names(want))
		}
		saved = got
	}
	if err := os.Remove(gi); err != nil { // and gone: a's time changes, its rules too
		t.Fatal(err)
	}
	got, _ := crawlSorted(t, root, prune, saved)
	if want, _ := crawlSorted(t, root, prune, nil); !slices.Equal(got, want) {
		t.Fatalf(".gitignore removed:\n got %v\nwant %v", names(got), names(want))
	}
}

// TestRefreshUnreadableThenReadable: a folder the earlier crawl could not
// read was saved with nothing below it. Made readable, its time does not
// change (chmod touches another one): Refresh must read it all the same.
func TestRefreshUnreadableThenReadable(t *testing.T) {
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	writeFile(t, filepath.Join(locked, "secret.txt"), "x")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) }) //nolint:errcheck,gosec // a folder must be searchable for TempDir to remove it
	saved, _ := crawlSorted(t, root, nil, nil)
	if len(saved) != 1 {
		t.Skip("the folder was readable anyway (running as root?)")
	}
	if err := os.Chmod(locked, 0o700); err != nil { //nolint:gosec // a folder: x is what lets it be entered
		t.Fatal(err)
	}
	got, _ := crawlSorted(t, root, nil, saved)
	if want, _ := crawlSorted(t, root, nil, nil); !slices.Equal(got, want) {
		t.Errorf("made readable:\n got %v\nwant %v", names(got), names(want))
	}
}
