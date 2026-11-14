package mutation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
)

// Errors of a copy.
var (
	// ErrIntoItself is a folder copied into itself, a/ to a/b/: the copy
	// would read the folder it writes to, and run until the disk is full.
	ErrIntoItself = errors.New("cannot copy a folder into itself")
	// ErrNotCopied is a pipe, socket or device: reading a pipe can wait
	// forever, and what a device would give is not a file.
	ErrNotCopied = errors.New("only files, folders and links are copied")
)

// copySeq makes the temporary names of copies unique within a process.
var copySeq atomic.Int64

// Progress is what a running Apply has copied so far: entries (files,
// folders, links) and bytes. Apply adds to it; another goroutine, the
// interface's, reads it while it waits. Atomic counters, not a callback:
// the copy never waits for whoever watches, and a watcher reads the latest
// numbers whenever it looks.
type Progress struct {
	Entries atomic.Int64
	Bytes   atomic.Int64
}

// copyChunk is how much of a file is copied between two looks at the
// context and the progress: a 5 GB file is stopped within a chunk, and
// shows it moving. Chunks through io.CopyN keep the kernel's own copy
// (copy_file_range on Linux), as io.LimitedReader over a file is
// unwrapped for it; a reader that counted every Read would lose it.
const copyChunk = 4 << 20

// copying is one copy: its context, to stop it, and where it counts.
type copying struct {
	ctx  context.Context
	prog *Progress // nil: nobody watches
}

func (c copying) count(entries, bytes int64) {
	if c.prog != nil {
		c.prog.Entries.Add(entries)
		c.prog.Bytes.Add(bytes)
	}
}

// copyEntry copies src to dst, which is not there: a file, a folder with
// all it holds, or a link as a link, never what it points to (a link to
// the home would copy the home). Modes and times are kept, as Finder
// keeps them.
//
// The copy is made under a hidden temporary name beside dst and renamed
// to dst only once whole: stopped halfway (no room, no permission), it is
// removed, and dst never holds half a copy. So is a copy stopped by ctx.
func copyEntry(ctx context.Context, src, dst string, prog *Progress) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { //nolint:gosec // the user's own folders: their umask decides
		return err
	}
	tmp := filepath.Join(filepath.Dir(dst), ".fndr-copy-"+strconv.Itoa(os.Getpid())+"-"+strconv.FormatInt(copySeq.Add(1), 10))
	if err := (copying{ctx, prog}).tree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp) //nolint:errcheck // the copy's error says more
		return err
	}
	if err := noReplace(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp) //nolint:errcheck // as above
		return err
	}
	return nil
}

// tree copies src to dst, which is not there, recursively.
func (c copying) tree(src, dst string) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch mode := info.Mode(); {
	case mode&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.Symlink(target, dst); err != nil {
			return err
		}
		c.count(1, 0)
		return nil
	case mode.IsDir():
		// Writable while it is filled; its own mode once it is.
		if err := os.Mkdir(dst, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := c.tree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		if err := os.Chmod(dst, mode.Perm()); err != nil {
			return err
		}
		if err := os.Chtimes(dst, info.ModTime(), info.ModTime()); err != nil {
			return err
		}
		c.count(1, 0)
		return nil
	case mode.IsRegular():
		return c.file(src, dst, info)
	default:
		return fmt.Errorf("%s: %w", filepath.Base(src), ErrNotCopied)
	}
}

// file copies the regular file src, described by info, to dst.
func (c copying) file(src, dst string, info os.FileInfo) error {
	in, err := os.Open(src) //nolint:gosec // the user's own file, chosen in the plan
	if err != nil {
		return err
	}
	defer in.Close()                                                                    //nolint:errcheck // read only
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm()) //nolint:gosec // as above
	if err != nil {
		return err
	}
	if err := c.chunks(out, in); err != nil {
		_ = out.Close() //nolint:errcheck // the copy's error says more
		return err
	}
	if err := out.Close(); err != nil { // a write the disk refused at the end
		return err
	}
	if err := os.Chtimes(dst, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	c.count(1, 0)
	return nil
}

// chunks copies in to out a chunk at a time, counting each, and stops
// between two when the context is done.
func (c copying) chunks(out, in *os.File) error {
	for {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		n, err := io.CopyN(out, in, copyChunk)
		c.count(0, n)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
