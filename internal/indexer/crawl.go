package indexer

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
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
//		stats, err = indexer.Crawl(ctx, root, prune, out)
//	}()
//	for batch := range out {
//		// the receiver owns batch from here on
//	}
//
// Each batch is a fresh slice that Crawl never touches again after sending,
// so the receiver may keep or modify it without copying. Batches come in no
// particular order: folders are read in parallel (see walker).
//
// Behaviour on the awkward cases:
//   - Symbolic links are recorded but never followed, so link cycles cannot
//     make the walk endless. A link to a directory has IsDir == false.
//   - Unreadable paths below root are counted in Stats.Skipped and the walk
//     continues; only an unreadable root is an error.
//   - .git directories are skipped: git internals, never what the user wants.
//   - Folders prune names (see Prune) are sent but not entered. The root is
//     never pruned. A nil prune enters everything but .git.
//   - Hidden files are included. Hiding them is a view decision, and dropping
//     them here would force a full re-crawl to show them later.
//
// On cancellation Crawl returns ctx.Err(), and Stats covers what was sent up
// to that point.
func Crawl(ctx context.Context, root string, prune *Prune, out chan<- []Entry) (Stats, error) {
	return crawl(ctx, root, prune, out, crawlWorkers())
}

// crawlWorkers is how many folders are read at once. The work is mostly
// waiting on the file system, not computing, so it is not bound to the
// number of cores; but past a point the disk and the kernel are the limit,
// and more workers only add contention. BenchmarkCrawlWorkers measures
// where that point is.
func crawlWorkers() int {
	return min(max(runtime.GOMAXPROCS(0), 4), 8)
}

func crawl(ctx context.Context, root string, prune *Prune, out chan<- []Entry, workers int) (Stats, error) {
	// Entries must hold absolute, cleaned paths (see Entry.Path); Abs gives
	// both.
	root, err := filepath.Abs(root)
	if err != nil {
		return Stats{}, fmt.Errorf("crawl: %w", err)
	}

	// Lstat, not Stat: a root that is itself a symlink is rejected rather
	// than silently followed, consistent with how links below it are treated.
	info, err := os.Lstat(root) //nolint:gosec // reading the folder the caller names is what Crawl is for
	if err != nil {
		// The *PathError already names the path; wrapping only adds where it
		// happened.
		return Stats{}, fmt.Errorf("crawl: %w", err)
	}
	if !info.IsDir() {
		return Stats{}, fmt.Errorf("crawl %s: not a directory", root)
	}

	w := &walker{ctx: ctx, root: root, prune: prune, out: out}
	w.cond = sync.NewCond(&w.mu)
	w.stack = []dirJob{{dir: root}}

	// Workers wait on the condition variable, which knows nothing of ctx.
	// AfterFunc stops the walk the moment ctx is done, waking them all.
	defer context.AfterFunc(ctx, w.stop)()

	var wg sync.WaitGroup
	for range workers {
		wg.Go(w.work)
	}
	wg.Wait()

	stats := Stats{Entries: int(w.entries.Load()), Skipped: int(w.skipped.Load())}
	if w.err != nil {
		return stats, w.err
	}
	return stats, ctx.Err()
}

// The walk reads folders in parallel. A shared stack holds the folders still
// to read; every worker takes one, reads it, sends its entries and puts its
// subfolders back. Most of a crawl is waiting on the file system (a Lstat
// per entry for size and time), so while one worker waits, others read: the
// disk serves many requests at once, and a single goroutine would hand it
// only one.
//
// Why a slice under a mutex and not a channel: workers both take folders
// and add them. With a bounded channel, every worker could block adding
// subfolders with none left to take them: a deadlock. Go has no unbounded
// channel; a slice grows as needed, and the condition variable lets idle
// workers sleep until there is work.
//
// A stack, last in first out, walks depth first: it holds the folders along
// the current paths, where a queue, breadth first, would hold every folder
// of a whole level of a wide tree at once.

// dirJob is a folder to read.
type dirJob struct {
	dir     string
	ignores ignoreStack // the .gitignore files above dir
}

// walker is the state of one crawl, shared by its workers.
type walker struct {
	ctx   context.Context // fine in a struct: it lives exactly as long as one crawl
	root  string
	prune *Prune
	out   chan<- []Entry

	mu    sync.Mutex
	cond  *sync.Cond // signalled when stack grows or the walk ends
	stack []dirJob
	busy  int  // folders taken and not yet finished: they may add more
	done  bool // nothing left to read, or the walk was stopped
	err   error

	entries, skipped atomic.Int64
}

// next takes a folder to read, waiting while there is none. It returns
// false once the walk is over.
//
// An empty stack alone does not mean the end: a worker reading a folder may
// be about to add its subfolders. The end is an empty stack with no folder
// in work, which finish notices.
func (w *walker) next() (dirJob, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(w.stack) == 0 && !w.done {
		w.cond.Wait()
	}
	if w.done {
		return dirJob{}, false
	}
	j := w.stack[len(w.stack)-1]
	w.stack = w.stack[:len(w.stack)-1]
	w.busy++
	return j, true
}

// finish hands back what reading a folder found to read next, and ends
// the walk if that was the last of it.
func (w *walker) finish(subdirs []dirJob) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stack = append(w.stack, subdirs...)
	w.busy--
	switch {
	case len(w.stack) == 0 && w.busy == 0:
		w.done = true
		w.cond.Broadcast() // everyone waiting can go home
	case len(subdirs) > 0:
		w.cond.Broadcast() // work for whoever is idle
	}
}

// stop ends the walk early: on cancellation, or an unreadable root.
func (w *walker) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.done = true
	w.cond.Broadcast()
}

// fail stops the walk with err, the first error only.
func (w *walker) fail(err error) {
	w.mu.Lock()
	if w.err == nil {
		w.err = err
	}
	w.mu.Unlock()
	w.stop()
}

// work is one worker: it reads folders until the walk is over. Its entries
// collect in a batch of its own, so workers never contend over each entry,
// only once per batchSize of them.
func (w *walker) work() {
	batch := make([]Entry, 0, batchSize)
	for {
		j, ok := w.next()
		if !ok {
			break
		}
		subdirs, ok := w.read(j, &batch)
		if !ok {
			return // canceled: the walk is stopped, and the batch is not wanted
		}
		w.finish(subdirs)
	}
	w.send(batch) // the last one, usually partial
}

// read reads one folder: its entries go into batch, sent whenever it is
// full, and its subfolders to enter are returned. It reports false if the
// walk was canceled.
func (w *walker) read(j dirJob, batch *[]Entry) ([]dirJob, bool) {
	des, err := os.ReadDir(j.dir)
	if err != nil {
		if j.dir == w.root {
			// Nothing at all can be indexed: the caller must hear of it.
			w.fail(fmt.Errorf("crawl: %w", err))
			return nil, true
		}
		// ReadDir returns what it read before the error: index that much.
		w.skipped.Add(1)
	}

	// The listing tells whether the folder has a .gitignore, so a folder
	// without one costs nothing: no file is opened to find out.
	ignores := j.ignores
	if w.prune != nil && w.prune.gitignore && hasFile(des, ".gitignore") {
		ignores = ignores.with(loadGitignore(j.dir))
	}

	var subdirs []dirJob
	for _, d := range des {
		// Checked per entry, so a canceled crawl stops within one Lstat,
		// not one batch: a few nanoseconds against a microsecond or more.
		if w.ctx.Err() != nil {
			return nil, false
		}
		path := filepath.Join(j.dir, d.Name())
		// Git internals: thousands of objects and refs nobody searches for.
		// Checked below the root only, so crawling a .git on purpose works.
		if d.IsDir() && d.Name() == ".git" {
			continue
		}
		// ReadDir knows the name and type from the folder itself; size and
		// modification time cost one Lstat per entry. That syscall is the
		// main cost of a crawl, and what the workers overlap.
		info, err := d.Info()
		if err != nil {
			// Typically deleted between the listing and the Lstat.
			w.skipped.Add(1)
			continue
		}
		*batch = append(*batch, NewEntry(path, info.Size(), info.ModTime().UnixNano(), d.IsDir()))
		if len(*batch) == batchSize {
			if !w.send(*batch) {
				return nil, false
			}
			*batch = make([]Entry, 0, batchSize)
		}
		// A pruned folder is listed, as above, and not entered.
		if d.IsDir() && !w.prune.Prunes(path, d.Name()) && !ignores.ignores(path) {
			subdirs = append(subdirs, dirJob{dir: path, ignores: ignores})
		}
	}
	return subdirs, true
}

// send hands a batch to the receiver, unless it is empty. It reports false
// if ctx was canceled first: without that case, a receiver that stopped
// reading would block the send forever and leak the worker.
//
// The receiver owns the slice once sent; the caller starts a new one.
func (w *walker) send(batch []Entry) bool {
	if len(batch) == 0 {
		return true
	}
	// Checked first: when both cases are ready, select picks one at
	// random, and a crawl canceled before it began would still send half
	// the time.
	if w.ctx.Err() != nil {
		return false
	}
	select {
	case w.out <- batch:
		w.entries.Add(int64(len(batch)))
		return true
	case <-w.ctx.Done():
		return false
	}
}

// hasFile reports whether des, a folder's listing, holds a file named name.
func hasFile(des []fs.DirEntry, name string) bool {
	for _, d := range des {
		if d.Name() == name {
			return !d.IsDir()
		}
	}
	return false
}
