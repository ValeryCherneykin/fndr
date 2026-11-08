package mutation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
func Apply(dir string, p Plan, trash *Trash) (done int, err error) {
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

// renameAll does the renames in two phases (see Apply). If the second
// phase fails, the items still under a temporary name go back to their old
// names, so nothing is ever left under a name the user did not choose.
func renameAll(dir string, ops []Op) (done int, err error) {
	temps := make([]string, len(ops))
	for i, op := range ops {
		temps[i] = filepath.Join(dir, ".fndr-rename-"+strconv.Itoa(os.Getpid())+"-"+strconv.Itoa(i))
		if err := noReplace(filepath.Join(dir, op.From), temps[i]); err != nil {
			undo(dir, ops[:i], temps)
			return 0, fmt.Errorf("rename %q: %w", op.From, err)
		}
	}

	for i, op := range ops {
		to := filepath.Join(dir, op.To)
		err := os.MkdirAll(filepath.Dir(to), 0o755) //nolint:gosec // the user's own folder: their umask decides
		if err == nil {
			err = noReplace(temps[i], to)
		}
		if err != nil {
			undo(dir, ops[i:], temps[i:])
			return i, fmt.Errorf("rename %q to %q: %w", op.From, op.To, err)
		}
	}
	return len(ops), nil
}

// undo moves items from their temporary names back to their old ones.
// It is best effort: it runs after a failure, and its own errors cannot be
// reported better than the first one.
func undo(dir string, ops []Op, temps []string) {
	for i, op := range ops {
		_ = noReplace(temps[i], filepath.Join(dir, op.From)) //nolint:errcheck // see above
	}
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
