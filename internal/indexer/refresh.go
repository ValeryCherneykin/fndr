package indexer

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Refresh is Crawl for a tree crawled before, cheaper where it has not
// changed. saved is that crawl's entries, sorted by path, as Load returns
// them and Under cuts them; the result is what Crawl would find now, sent
// the same way.
//
// It rests on one fact of file systems: a folder's modification time
// changes whenever a name inside it is created, removed or renamed. A
// folder whose time is the one saved holds the same names, so its entries
// are taken from saved instead of read: no listing, only a Lstat per
// entry. Folders below are still checked one by one: a change deep down
// does not touch the times above it.
//
// What a folder's time does not see: a file written to in place, as
// editors often save. So every file is looked at too, for its size and
// time: without it, @today missed the file edited this morning and
// size>1mb went by its old size. It doubles a refresh of a home (46 to 91
// ms, measured), which runs in the background, and is still half a crawl.
//
// The saved time of a folder is never newer than the listing it vouches
// for: the crawl reads a folder's time when listing its parent, before
// listing the folder itself. A change in between makes the next Refresh
// read the folder again, never miss it.
func Refresh(ctx context.Context, root string, prune *Prune, saved []Entry, out chan<- []Entry) (Stats, error) {
	if saved == nil {
		saved = []Entry{} // nil means "no earlier crawl" to the walker
	}
	return crawl(ctx, root, prune, out, crawlWorkers(), saved)
}

// subJob is the job for path, a folder below j's that the crawl enters,
// with mod its time now, and what the earlier crawl knew of it, if any.
func subJob(j *dirJob, path string, mod int64, ignores ignoreStack) dirJob {
	sub := dirJob{dir: path, ignores: ignores, mod: mod}
	if j.saved == nil {
		return sub // a plain crawl
	}
	sub.saved = Under(j.saved, path) // empty, not nil: Refresh knows nothing below
	if sub.saved == nil {
		sub.saved = []Entry{}
	}
	if e, ok := find(j.saved, path); ok && e.IsDir {
		// Entered then as now: the rules are the same (Saved.Rules), so
		// a folder with nothing saved below it was an empty one.
		sub.known, sub.savedMod = true, e.ModTime
	}
	return sub
}

// find returns the entry of sorted at path.
func find(sorted []Entry, path string) (Entry, bool) {
	i, ok := slices.BinarySearchFunc(sorted, path, func(e Entry, p string) int { return strings.Compare(e.Path, p) })
	if !ok {
		return Entry{}, false
	}
	return sorted[i], true
}

// unchanged reports whether j's folder holds what the earlier crawl saw,
// so its entries can be reused: the same time, and its .gitignore, if it
// had one, not edited. Editing a .gitignore does not change its folder's
// time, yet it decides which folders below are entered.
func (w *walker) unchanged(j *dirJob) bool {
	// Nothing saved below it: an empty folder then, or one that could not
	// be read. The second may be readable now, and making it so (chmod)
	// does not change its time: read it. An empty listing costs little.
	if !j.known || j.mod != j.savedMod || len(j.saved) == 0 {
		return false
	}
	// The same time: no name came or went, so it has a .gitignore if it
	// had one. Whether that one was edited is the question left.
	_, had := find(j.saved, filepath.Join(j.dir, ".gitignore"))
	return !w.ignoresChanged(j, had)
}

// ignoresChanged reports whether the .gitignore of j's folder is not as
// the earlier crawl saw it: made, removed or edited since. has says if
// the folder has one now. Then nothing saved below the folder can be
// trusted: a folder that was ignored was listed and not entered, and
// looks just like an empty one that never changed.
func (w *walker) ignoresChanged(j *dirJob, has bool) bool {
	if j.saved == nil || w.prune == nil || !w.prune.gitignore {
		return false
	}
	gi, had := find(j.saved, filepath.Join(j.dir, ".gitignore"))
	if had != has {
		return true
	}
	if !had {
		return false
	}
	info, err := os.Lstat(gi.Path)
	return err != nil || info.Size() != gi.Size || info.ModTime().UnixNano() != gi.ModTime
}

// reuse is read for a folder that has not changed: its entries come from
// the earlier crawl. The folders among them are looked at, though: their
// time now, for the entry and for their own job.
func (w *walker) reuse(j *dirJob, batch *[]Entry) ([]dirJob, bool) {
	w.reused.Add(1)

	ignores := j.ignores
	if w.prune != nil && w.prune.gitignore {
		if gi, ok := find(j.saved, filepath.Join(j.dir, ".gitignore")); ok && !gi.IsDir {
			ignores = ignores.with(loadGitignore(j.dir))
		}
	}

	prefix := len(j.dir) + 1
	if j.dir == string(filepath.Separator) {
		prefix = 1
	}
	var subdirs []dirJob
	for i := 0; i < len(j.saved); {
		if w.ctx.Err() != nil {
			return nil, false
		}
		e := j.saved[i]
		if k := strings.IndexByte(e.Path[prefix:], filepath.Separator); k >= 0 {
			// Below a subfolder: its job takes those. Skip them all.
			end := e.Path[:prefix+k] + string(rune(filepath.Separator+1))
			n, _ := slices.BinarySearchFunc(j.saved[i:], end, func(e Entry, p string) int { return strings.Compare(e.Path, p) })
			i += max(n, 1)
			continue
		}
		i++
		if e.IsDir {
			info, err := os.Lstat(e.Path)
			if err != nil || !info.IsDir() {
				// Gone, or a file now, though the folder's time says
				// nothing changed: a clock that went back. Leave it out;
				// the next change to the folder brings the truth.
				w.skipped.Add(1)
				continue
			}
			mod := info.ModTime().UnixNano()
			e = NewEntry(e.Path, info.Size(), mod, true)
			if !w.prune.Prunes(e.Path, e.Name()) && !ignores.ignores(e.Path) {
				subdirs = append(subdirs, subJob(j, e.Path, mod, ignores))
			}
		} else if info, err := os.Lstat(e.Path); err == nil && !info.IsDir() {
			// Written in place since, maybe: the folder's time cannot tell.
			// Gone or a folder now, with the folder's time the same, is a
			// clock gone back; as above, the next change brings the truth.
			e = NewEntry(e.Path, info.Size(), info.ModTime().UnixNano(), false)
		}
		*batch = append(*batch, e)
		if len(*batch) == batchSize {
			if !w.send(*batch) {
				return nil, false
			}
			*batch = make([]Entry, 0, batchSize)
		}
	}
	return subdirs, true
}
