package indexer

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// batchSize is how many entries Crawl groups into one channel send. Large
// enough that the cost of a send disappears next to the syscalls behind each
// entry, small enough that the first results reach the UI within
// milliseconds of starting a crawl.
const batchSize = 1024

// Stats summarizes a crawl.
type Stats struct {
	// Entries is the number of entries sent to the output channel, files and
	// directories alike.
	Entries int

	// Skipped is the number of paths that could not be read: directories
	// without read permission, files deleted while the crawl was running.
	// Whatever was below them is missing from the index, so the UI should
	// report this number instead of failing silently.
	Skipped int
}

// Crawl walks the tree below root and sends every file and directory it finds
// to out, in batches of up to batchSize entries. The root itself is not sent:
// the index describes the contents of a directory.
//
// Crawl is synchronous: it returns when the walk is complete, ctx is
// canceled, or root cannot be read. It never closes out, so the caller decides
// how to run it and owns the channel's lifetime:
//
//	out := make(chan []Entry)
//	go func() {
//		defer close(out)
//		stats, err = indexer.Crawl(ctx, root, out)
//	}()
//	for batch := range out {
//		// the receiver owns batch from here on
//	}
//
// Each batch is a fresh slice that Crawl never touches again after sending,
// so the receiver may keep or modify it without copying.
//
// Behaviour on the awkward cases:
//   - Symbolic links are recorded but never followed, so link cycles cannot
//     make the walk endless. A link to a directory has IsDir == false.
//   - Unreadable paths below root are counted in Stats.Skipped and the walk
//     continues; only an unreadable root is an error.
//   - .git directories are skipped: git internals, never what the user wants.
//   - Hidden files are included. Hiding them is a view decision, and dropping
//     them here would force a full re-crawl to show them later.
//
// On cancellation Crawl returns ctx.Err(), and Stats covers what was sent up
// to that point.
func Crawl(ctx context.Context, root string, out chan<- []Entry) (Stats, error) {
	// Entries must hold absolute, cleaned paths (see Entry.Path); Abs gives
	// both.
	root, err := filepath.Abs(root)
	if err != nil {
		return Stats{}, fmt.Errorf("crawl: %w", err)
	}

	// Lstat, not Stat: a root that is itself a symlink is rejected rather
	// than silently followed, consistent with how links below it are treated.
	info, err := os.Lstat(root)
	if err != nil {
		// The *PathError already names the path; wrapping only adds where it
		// happened.
		return Stats{}, fmt.Errorf("crawl: %w", err)
	}
	if !info.IsDir() {
		return Stats{}, fmt.Errorf("crawl %s: not a directory", root)
	}

	c := &crawler{
		ctx:   ctx,
		root:  root,
		out:   out,
		batch: make([]Entry, 0, batchSize),
	}
	if err := filepath.WalkDir(root, c.visit); err != nil {
		return c.stats, err
	}
	// The last batch is usually partial; without this flush up to
	// batchSize-1 entries would be lost.
	return c.stats, c.flush()
}

// crawler holds the state of one Crawl call. It is used by a single goroutine
// only, so it needs no locking.
type crawler struct {
	ctx   context.Context // fine in a struct: it lives exactly as long as one Crawl call
	root  string
	out   chan<- []Entry
	batch []Entry
	stats Stats
}

// visit is the filepath.WalkDir callback. Returning nil continues the walk;
// returning an error stops it and makes WalkDir return that error.
func (c *crawler) visit(path string, d fs.DirEntry, err error) error {
	if err != nil {
		// WalkDir reports a failed directory read by calling us a second
		// time for that directory with the error. For the root that means
		// there is nothing to index at all, which the caller must hear about.
		if path == c.root {
			return fmt.Errorf("crawl: %w", err)
		}
		c.stats.Skipped++
		return nil
	}

	// Checked per entry so that cancellation is noticed within one entry,
	// not one batch. ctx.Err is a few nanoseconds; the Lstat behind
	// d.Info below costs a microsecond or more.
	if err := c.ctx.Err(); err != nil {
		return err
	}

	if path == c.root {
		return nil
	}

	// Git internals: thousands of objects and refs nobody searches for,
	// crowding real files out of the results. Checked after the root, so
	// crawling a .git directory on purpose still works.
	if d.IsDir() && d.Name() == ".git" {
		return fs.SkipDir
	}

	// WalkDir already knows the name and type from reading the directory;
	// size and modification time cost one extra Lstat per entry. That
	// syscall is the main cost of a crawl.
	info, err := d.Info()
	if err != nil {
		// Typically the file was deleted between the directory read and the
		// Lstat. Not worth failing the crawl over.
		c.stats.Skipped++
		return nil
	}

	c.batch = append(c.batch, NewEntry(path, info.Size(), info.ModTime().UnixNano(), d.IsDir()))
	if len(c.batch) == batchSize {
		return c.flush()
	}
	return nil
}

// flush sends the current batch, unless it is empty, and starts a new one.
func (c *crawler) flush() error {
	if len(c.batch) == 0 {
		return nil
	}

	// Without the ctx case, a receiver that stopped reading would block this
	// send forever and leak the goroutine running Crawl.
	select {
	case c.out <- c.batch:
	case <-c.ctx.Done():
		return c.ctx.Err()
	}

	c.stats.Entries += len(c.batch)

	// The receiver owns the sent slice now. Reusing it with c.batch[:0]
	// would overwrite entries the receiver is still reading: a data race.
	c.batch = make([]Entry, 0, batchSize)
	return nil
}
