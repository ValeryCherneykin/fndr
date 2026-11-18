package mutation

import (
	"errors"
	"io/fs"
	"os"
)

// The destination must be checked by the same kernel operation that renames it.
func noReplace(from, to string) error {
	if err := renameExclusive(from, to); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrExists
		}
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}
