package indexer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
)

// crawlAll runs Crawl the way real callers do, in its own goroutine, and
// collects everything it sends.
func crawlAll(tb testing.TB, root string) ([]Entry, Stats, error) {
	tb.Helper()

	out := make(chan []Entry)
	var (
		stats Stats
		err   error
	)
	go func() {
		// stats and err are written before close(out), and read only after
		// the range loop below sees the close, so the close orders the write
		// before the read and there is no race.
		defer close(out)
		stats, err = Crawl(context.Background(), root, out)
	}()

	var all []Entry
	for batch := range out {
		all = append(all, batch...)
	}
	return all, stats, err
}

// writeFile creates a file with the given content, creating parent
// directories as needed.
func writeFile(tb testing.TB, path, content string) {
	tb.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		tb.Fatal(err)
	}
}

// byRel indexes entries by their path relative to root, for readable
// assertions.
func byRel(tb testing.TB, root string, entries []Entry) map[string]Entry {
	tb.Helper()
	m := make(map[string]Entry, len(entries))
	for _, e := range entries {
		rel, err := filepath.Rel(root, e.Path)
		if err != nil {
			tb.Fatal(err)
		}
		if _, dup := m[rel]; dup {
			tb.Errorf("entry %q sent twice", rel)
		}
		m[rel] = e
	}
	return m
}

func TestCrawl(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main")
	writeFile(t, filepath.Join(root, ".hidden"), "")
	writeFile(t, filepath.Join(root, "cmd", "fndr", "run.go"), "12345")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o750); err != nil {
		t.Fatal(err)
	}

	entries, stats, err := crawlAll(t, root)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	got := byRel(t, root, entries)
	want := map[string]bool{ // relative path -> IsDir
		"main.go":         false,
		".hidden":         false,
		"cmd":             true,
		"cmd/fndr":        true,
		"cmd/fndr/run.go": false,
		"empty":           true,
	}

	for rel, isDir := range want {
		e, ok := got[filepath.FromSlash(rel)]
		if !ok {
			t.Errorf("missing entry %q", rel)
			continue
		}
		if e.IsDir != isDir {
			t.Errorf("%q: IsDir = %v, want %v", rel, e.IsDir, isDir)
		}
		if !filepath.IsAbs(e.Path) {
			t.Errorf("%q: Path %q is not absolute", rel, e.Path)
		}
		if e.Name() != filepath.Base(rel) {
			t.Errorf("%q: Name() = %q, want %q", rel, e.Name(), filepath.Base(rel))
		}
		if e.ModTime == 0 {
			t.Errorf("%q: ModTime not set", rel)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d entries, want %d (root must not be included): %v", len(got), len(want), slices.Sorted(maps.Keys(got)))
	}

	if e := got[filepath.Join("cmd", "fndr", "run.go")]; e.Size != 5 {
		t.Errorf("run.go: Size = %d, want 5", e.Size)
	}
	if stats.Entries != len(want) || stats.Skipped != 0 {
		t.Errorf("stats = %+v, want {Entries:%d Skipped:0}", stats, len(want))
	}
}

func TestCrawlDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "file.txt"), "")
	// a/loop points back at a: following it would recurse forever.
	if err := os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "a", "loop")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	entries, _, err := crawlAll(t, root)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	got := byRel(t, root, entries)
	link, ok := got[filepath.Join("a", "loop")]
	if !ok {
		t.Fatal("symlink itself must be indexed")
	}
	if link.IsDir {
		t.Error("symlink to a directory must have IsDir == false")
	}
	if len(got) != 3 { // a, a/file.txt, a/loop
		t.Errorf("got %d entries, want 3; was the link followed? %v", len(got), slices.Sorted(maps.Keys(got)))
	}
}

// TestCrawlSkipsGitDirs: a repository's .git holds thousands of objects and
// refs nobody searches for, and they crowd real files out of the results.
func TestCrawlSkipsGitDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "fndr", "main.go"), "")
	writeFile(t, filepath.Join(root, "fndr", ".gitignore"), "")
	writeFile(t, filepath.Join(root, "fndr", ".git", "HEAD"), "")
	writeFile(t, filepath.Join(root, "fndr", ".git", "refs", "heads", "main"), "")

	entries, _, err := crawlAll(t, root)
	if err != nil {
		t.Fatal(err)
	}
	got := slices.Sorted(maps.Keys(byRel(t, root, entries)))
	want := []string{"fndr", "fndr/.gitignore", "fndr/main.go"}
	if !slices.Equal(got, want) {
		t.Errorf("entries = %q, want %q", got, want)
	}

	// Crawling a .git directory itself is an explicit request: honour it.
	entries, _, err = crawlAll(t, filepath.Join(root, "fndr", ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 { // HEAD, refs, refs/heads, refs/heads/main
		t.Errorf("crawling .git as root: %d entries, want its contents", len(entries))
	}
}

func TestCrawlSkipsUnreadableDirs(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ok.txt"), "")
	writeFile(t, filepath.Join(root, "locked", "secret.txt"), "")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	// Restore permissions, or t.TempDir cannot clean up.
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) }) //nolint:gosec // a directory needs the x bit to be emptied

	entries, stats, err := crawlAll(t, root)
	if err != nil {
		t.Fatalf("an unreadable subdirectory must not fail the crawl: %v", err)
	}

	got := byRel(t, root, entries)
	if _, ok := got["ok.txt"]; !ok {
		t.Error("siblings of an unreadable directory must still be indexed")
	}
	if _, ok := got["locked"]; !ok {
		t.Error("the unreadable directory itself is listable and must be indexed")
	}
	if _, ok := got[filepath.Join("locked", "secret.txt")]; ok {
		t.Error("contents of an unreadable directory cannot be indexed")
	}
	if stats.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", stats.Skipped)
	}
}

func TestCrawlBatches(t *testing.T) {
	root := t.TempDir()
	const n = 2*batchSize + 7 // two full batches and a partial one
	for i := range n {
		writeFile(t, filepath.Join(root, fmt.Sprintf("f%04d", i)), "")
	}

	out := make(chan []Entry)
	go func() {
		defer close(out)
		_, _ = Crawl(context.Background(), root, out)
	}()

	var sizes []int
	for batch := range out {
		sizes = append(sizes, len(batch))
	}
	if want := []int{batchSize, batchSize, 7}; !slices.Equal(sizes, want) {
		t.Errorf("batch sizes = %v, want %v", sizes, want)
	}
}

func TestCrawlRootErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	writeFile(t, file, "")

	tests := []struct {
		name string
		root string
		is   error // checked with errors.Is when non-nil
	}{
		{name: "missing", root: filepath.Join(dir, "nope"), is: fs.ErrNotExist},
		{name: "file", root: file},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := crawlAll(t, tt.root)
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("err = %v, want errors.Is(err, %v)", err, tt.is)
			}
		})
	}
}

// TestCrawlCancelUnblocksSend checks the goroutine-leak case: the receiver
// stops reading, then cancels. Crawl must return instead of blocking on the
// send forever.
//
// synctest runs the test in a bubble where synctest.Wait returns once every
// goroutine in it is blocked, so the test needs no sleeps and cannot flake on
// a slow machine.
func TestCrawlCancelUnblocksSend(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "")

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		out := make(chan []Entry) // unbuffered and never read

		var err error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, err = Crawl(ctx, root, out)
		}()

		synctest.Wait() // Crawl is now stuck sending its only batch
		select {
		case <-done:
			t.Fatal("Crawl returned although nobody received its batch")
		default:
		}

		cancel()
		<-done // would deadlock here, and synctest would report it, if Crawl leaked
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestCrawlCanceledBeforeStart(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := make(chan []Entry, 1)
	stats, err := Crawl(ctx, root, out)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if stats.Entries != 0 || len(out) != 0 {
		t.Errorf("a canceled crawl sent entries: stats=%+v, queued=%d", stats, len(out))
	}
}

// BenchmarkCrawl measures a cold-start crawl of a synthetic tree. This is the
// baseline the parallel crawler has to beat. Compare runs with benchstat:
//
//	go test -run=^$ -bench=Crawl -count=10 ./internal/indexer > old.txt
//	# change the code
//	go test -run=^$ -bench=Crawl -count=10 ./internal/indexer > new.txt
//	benchstat old.txt new.txt
//
// The tree stays in the OS page cache between iterations, so this measures
// syscall and Go overhead, not disk speed.
func BenchmarkCrawl(b *testing.B) {
	root := b.TempDir()
	// 10 x 10 directories with 100 files each: 10,000 files, 110 directories.
	for i := range 10 {
		for j := range 10 {
			dir := filepath.Join(root, fmt.Sprintf("d%d", i), fmt.Sprintf("d%d", j))
			for k := range 100 {
				writeFile(b, filepath.Join(dir, fmt.Sprintf("f%d.go", k)), "")
			}
		}
	}

	var n int
	for b.Loop() {
		entries, _, err := crawlAll(b, root)
		if err != nil {
			b.Fatal(err)
		}
		n = len(entries)
	}
	b.ReportMetric(float64(n), "entries/op")
}

// BenchmarkCrawlDir crawls a real folder, named by FNDR_BENCH_DIR, and is
// skipped without it. BenchmarkCrawl's tree is regular and wide, a hundred
// files to a folder; a real home has eight, and much of the time goes on
// opening folders, which only a real tree shows. Compare runs with
// benchstat; a whole home takes seconds a run, so use -benchtime 1x:
//
//	FNDR_BENCH_DIR=$HOME go test -run '^$' -bench CrawlDir -benchtime 1x -count 5 ./internal/indexer
func BenchmarkCrawlDir(b *testing.B) {
	root := os.Getenv("FNDR_BENCH_DIR")
	if root == "" {
		b.Skip("FNDR_BENCH_DIR is not set")
	}
	var n int
	for b.Loop() {
		entries, _, err := crawlAll(b, root)
		if err != nil {
			b.Fatal(err)
		}
		n = len(entries)
	}
	b.ReportMetric(float64(n), "entries/op")
}
