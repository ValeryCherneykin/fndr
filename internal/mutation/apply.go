package mutation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Apply carries out a plan from Diff in dir, in the plan's order: deletes
// to trash, then copies, then renames, then creates. It stops at the first failure and
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
	return ApplyContext(context.Background(), dir, p, trash, nil)
}

// ApplyContext is Apply that ctx can stop, and that counts in prog, if not
// nil, what its copies have done so far, for the interface to show. Only a
// copy, the one step that can take minutes, is stopped halfway, and leaves
// nothing of itself; a rename is never left with half its names. Done
// before the first step, the plan is not started.
func ApplyContext(ctx context.Context, dir string, p Plan, trash *Trash, prog *Progress) (done int, err error) {
	steps, err := Run(ctx, dir, p, trash, prog)
	return len(steps), err
}

// Run is ApplyContext that returns, instead of a count, the steps it took,
// in order, as Undo takes them back: also when it stops at a failure, so
// the part done can be undone as well.
func Run(ctx context.Context, dir string, p Plan, trash *Trash, prog *Progress) (steps []Step, err error) {
	return RunIntent(ctx, Prepare(dir, p), trash, prog)
}

// RunIntent is Run of a plan prepared, and written down, before: it does
// exactly what in says, the temporary names included, so that Recover,
// reading the same in after a crash, knows where to look.
func RunIntent(ctx context.Context, in Intent, trash *Trash, prog *Progress) (steps []Step, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, p := in.Dir, in.Plan
	if err := check(dir, p); err != nil {
		return nil, err
	}
	var renames []int
	for i, op := range p {
		switch op.Kind {
		case OpDelete:
			from := filepath.Join(dir, op.From)
			to, err := trash.Put(from)
			if err != nil {
				return steps, err
			}
			crashPoint()
			steps = append(steps, took(OpDelete, from, to, ""))
		case OpRename:
			renames = append(renames, i)
		}
	}
	// Copies after the deletes, before the renames: a copy of an entry
	// also renamed in the plan is of the entry as it was, still there.
	for i, op := range p {
		if op.Kind != OpCopy {
			continue
		}
		from, to := Target(dir, op.From), Target(dir, op.To)
		if err := copyEntry(ctx, from, to, prog); err != nil {
			return steps, fmt.Errorf("copy %q to %q: %w", op.From, op.To, err)
		}
		crashPoint()
		steps = append(steps, took(OpCopy, from, to, in.Ops[i].Made))
	}

	ops, temps := make([]Op, len(renames)), make([]string, len(renames))
	for k, i := range renames {
		ops[k], temps[k] = p[i], in.Ops[i].Temp
	}
	n, err := renameAll(dir, ops, temps)
	for _, i := range renames[:n] {
		steps = append(steps, took(OpRename, filepath.Join(dir, p[i].From), Target(dir, p[i].To), in.Ops[i].Made))
	}
	if err != nil {
		return steps, err
	}

	for i, op := range p {
		if op.Kind != OpCreate {
			continue
		}
		c, err := Create(dir, op.To)
		if err != nil {
			return steps, err
		}
		crashPoint()
		steps = append(steps, took(OpCreate, "", c.Path, in.Ops[i].Made))
	}
	return steps, nil
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
	// What leaves its name, and by when: deletes run before the copies,
	// renames after them. A copy's name must be free once the deletes are
	// done; the others' once the renames are too.
	var leaving, deleted []os.FileInfo
	var trashed []string // the paths deleted, which a copy may not come from
	for _, op := range p {
		if op.Kind == OpCreate {
			continue
		}
		info, err := os.Lstat(Target(dir, op.From))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				err = fs.ErrNotExist // the path is in the message already, short
			}
			return fmt.Errorf("%q: %w", op.From, err)
		}
		switch op.Kind {
		case OpDelete:
			deleted = append(deleted, info)
			leaving = append(leaving, info)
			trashed = append(trashed, filepath.Join(dir, op.From))
		case OpRename:
			leaving = append(leaving, info)
		case OpCopy: // stays where it is
			src, dst := Target(dir, op.From), Target(dir, op.To)
			if info.IsDir() && isWithin(dst, src) {
				return fmt.Errorf("copy %q to %q: %w", op.From, op.To, ErrIntoItself)
			}
			// Yanked elsewhere, it may lie in a folder this plan trashes,
			// which Diff cannot see from names: deletes run first, and the
			// copy would find nothing.
			for _, t := range trashed {
				if isWithin(src, t) {
					return fmt.Errorf("copy %q: %w", op.From, ErrNested)
				}
			}
		}
	}
	for _, op := range p {
		var err error
		switch op.Kind {
		case OpDelete:
			continue
		case OpCopy:
			if err = free(dir, op.To, deleted); err != nil {
				return fmt.Errorf("copy %q to %q: %w", op.From, op.To, err)
			}
		case OpRename:
			if err = free(dir, op.To, leaving); err != nil {
				return fmt.Errorf("rename %q to %q: %w", op.From, op.To, err)
			}
		default:
			if err = free(dir, op.To, leaving); err != nil {
				return fmt.Errorf("create %q: %w", op.To, err)
			}
		}
	}
	return nil
}

// isWithin reports whether path is dir or lies below it; both cleaned.
func isWithin(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// free says whether name can be taken in dir. Walking it from the top,
// each part may be missing (then the rest is made new), leaving, or a
// folder to go through; the last part must not be there at all.
func free(dir, name string, leaving []os.FileInfo) error {
	// A move away (an absolute name) is walked from the root.
	path, rel := dir, filepath.Clean(name)
	if filepath.IsAbs(rel) {
		path, rel = string(filepath.Separator), rel[1:]
	}
	parts := strings.Split(rel, string(filepath.Separator))
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
func renameAll(dir string, ops []Op, temps []string) (done int, err error) {
	for i, op := range ops {
		if err := noReplace(filepath.Join(dir, op.From), temps[i]); err != nil {
			return 0, withUndo(fmt.Errorf("rename %q: %w", op.From, err), undo(dir, ops[:i], temps))
		}
		crashPoint()
	}

	for i, op := range ops {
		to := Target(dir, op.To)
		err := os.MkdirAll(filepath.Dir(to), 0o755) //nolint:gosec // the user's own folder: their umask decides
		if err == nil {
			err = noReplace(temps[i], to)
		}
		if err != nil {
			return i, withUndo(fmt.Errorf("rename %q to %q: %w", op.From, op.To, err), undo(dir, ops[i:], temps[i:]))
		}
		crashPoint()
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
