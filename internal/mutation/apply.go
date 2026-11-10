package mutation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Apply carries out a plan from Diff in dir, in the plan's order: deletes
// to trash, then renames, then creates. It stops at the first failure and
// returns how many operations were done, so the interface can say exactly
// what happened: a half-applied plan the user cannot see is worse than an
// error.
//
// Renames run in two phases. Every renamed item first moves to a temporary
// name, then from there to its new name. Done one by one, a swap (a→b,
// b→a) would fail on the first step, b being taken, and a chain (a→b,
// b→c) would depend on the order; in two phases all old names are free
// before any new one is taken.
//
// Before the first step, check makes sure every new name is free, so what
// can be found out in advance never stops a plan halfway.
func Apply(dir string, p Plan, trash *Trash) (done int, err error) {
	if err := check(dir, p); err != nil {
		return 0, err
	}
	var renames []Op
	for _, op := range p {
		switch op.Kind {
		case OpDelete:
			if _, err := trash.Put(filepath.Join(dir, op.From)); err != nil {
				return done, err
			}
			done++
		case OpRename:
			renames = append(renames, op)
		}
	}

	n, err := renameAll(dir, renames)
	done += n
	if err != nil {
		return done, err
	}

	for _, op := range p {
		if op.Kind != OpCreate {
			continue
		}
		if _, err := Create(dir, op.To); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// check makes sure, before Apply changes anything, that every name the
// plan takes is free: nothing is there, or what is there leaves in the
// same plan, by a delete or a rename away. Diff cannot know it, as it sees
// the buffer and not the disk: a hidden file left out of the buffer may
// hold the name, or a file may stand where a path needs a folder.
//
// What leaves is told by os.SameFile, not by comparing names: on macOS
// "A.txt" opens "a.txt", and only the file system knows which names it
// takes for one, letter case, Unicode normalization and all.
//
// Two new names the file system takes for one ("A.txt" and "a.txt", both
// created) still fail inside Apply: neither exists yet to compare. Nor
// does a check close the gap before the step itself; that is noReplace's
// and O_EXCL's job.
func check(dir string, p Plan) error {
	var leaving []os.FileInfo
	for _, op := range p {
		if op.Kind == OpCreate {
			continue
		}
		info, err := os.Lstat(filepath.Join(dir, op.From))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				err = fs.ErrNotExist // the path is in the message already, short
			}
			return fmt.Errorf("%q: %w", op.From, err)
		}
		leaving = append(leaving, info)
	}
	for _, op := range p {
		if op.Kind == OpDelete {
			continue
		}
		if err := free(dir, op.To, leaving); err != nil {
			if op.Kind == OpRename {
				return fmt.Errorf("rename %q to %q: %w", op.From, op.To, err)
			}
			return fmt.Errorf("create %q: %w", op.To, err)
		}
	}
	return nil
}

// free says whether name can be taken in dir. Walking it from the top,
// each part may be missing (then the rest is made new), leaving, or a
// folder to go through; the last part must not be there at all.
func free(dir, name string, leaving []os.FileInfo) error {
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	path := dir
	for i, part := range parts {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return err
		case slices.ContainsFunc(leaving, func(l os.FileInfo) bool { return os.SameFile(l, info) }):
			return nil // gone before anything is put here, and all below it
		case i == len(parts)-1:
			return ErrExists
		}
		// MkdirAll goes through a link to a folder, so the check does too.
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return fmt.Errorf("%s: %w", filepath.Join(parts[:i+1]...), ErrNotFolder)
		}
	}
	return nil
}

// renameAll does the renames in two phases (see Apply). If the second
// phase fails, the items still under a temporary name go back to their old
// names, so nothing is ever left under a name the user did not choose.
func renameAll(dir string, ops []Op) (done int, err error) {
	temps := make([]string, len(ops))
	for i, op := range ops {
		temps[i] = filepath.Join(dir, ".fndr-rename-"+strconv.Itoa(os.Getpid())+"-"+strconv.Itoa(i))
		if err := noReplace(filepath.Join(dir, op.From), temps[i]); err != nil {
			return 0, withUndo(fmt.Errorf("rename %q: %w", op.From, err), undo(dir, ops[:i], temps))
		}
	}

	for i, op := range ops {
		to := filepath.Join(dir, op.To)
		err := os.MkdirAll(filepath.Dir(to), 0o755) //nolint:gosec // the user's own folder: their umask decides
		if err == nil {
			err = noReplace(temps[i], to)
		}
		if err != nil {
			return i, withUndo(fmt.Errorf("rename %q to %q: %w", op.From, op.To, err), undo(dir, ops[i:], temps[i:]))
		}
	}
	return len(ops), nil
}

// undo moves items from their temporary names back to their old ones. An
// item it cannot move back stays where it is, and the error names it: the
// temporary name is hidden, and a file the user cannot see is a file lost.
func undo(dir string, ops []Op, temps []string) error {
	var left []string
	for i, op := range ops {
		if noReplace(temps[i], filepath.Join(dir, op.From)) != nil {
			left = append(left, fmt.Sprintf("%q is left as %q", op.From, filepath.Base(temps[i])))
		}
	}
	if len(left) == 0 {
		return nil
	}
	// One line, unlike errors.Join: it is read in a status bar.
	return errors.New(strings.Join(left, ", "))
}

// withUndo adds to err what undo could not put back, if anything.
func withUndo(err, undoErr error) error {
	if undoErr == nil {
		return err
	}
	return fmt.Errorf("%w; %w", err, undoErr)
}

// noReplace renames from to to, refusing if to exists. os.Rename alone
// would silently replace a file there.
//
// The check and the rename are two steps: a file created at to in between
// would be replaced. The kernels can do both at once (renameat2 with
// RENAME_NOREPLACE on Linux, renamex_np with RENAME_EXCL on macOS), but
// not through the standard library; in a folder the user is editing, the
// gap is a few microseconds wide.
func noReplace(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return ErrExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(from, to)
}
