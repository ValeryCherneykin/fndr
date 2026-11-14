package mutation

import (
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

// copyEntry copies src to dst, which is not there: a file, a folder with
// all it holds, or a link as a link, never what it points to (a link to
// the home would copy the home). Modes and times are kept, as Finder
// keeps them.
//
// The copy is made under a hidden temporary name beside dst and renamed
// to dst only once whole: stopped halfway (no room, no permission), it is
// removed, and dst never holds half a copy.
func copyEntry(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { //nolint:gosec // the user's own folders: their umask decides
		return err
	}
	tmp := filepath.Join(filepath.Dir(dst), ".fndr-copy-"+strconv.Itoa(os.Getpid())+"-"+strconv.FormatInt(copySeq.Add(1), 10))
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp) //nolint:errcheck // the copy's error says more
		return err
	}
	if err := noReplace(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp) //nolint:errcheck // as above
		return err
	}
	return nil
}

// copyTree copies src to dst, which is not there, recursively.
func copyTree(src, dst string) error {
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
		return os.Symlink(target, dst)
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
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		if err := os.Chmod(dst, mode.Perm()); err != nil {
			return err
		}
		return os.Chtimes(dst, info.ModTime(), info.ModTime())
	case mode.IsRegular():
		return copyFile(src, dst, info)
	default:
		return fmt.Errorf("%s: %w", filepath.Base(src), ErrNotCopied)
	}
}

// copyFile copies the regular file src, described by info, to dst.
func copyFile(src, dst string, info os.FileInfo) error {
	in, err := os.Open(src) //nolint:gosec // the user's own file, chosen in the plan
	if err != nil {
		return err
	}
	defer in.Close()                                                                    //nolint:errcheck // read only
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm()) //nolint:gosec // as above
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close() //nolint:errcheck // the copy's error says more
		return err
	}
	if err := out.Close(); err != nil { // a write the disk refused at the end
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}
