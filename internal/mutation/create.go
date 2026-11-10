// Package mutation changes the disk on the user's behalf: creating,
// renaming and deleting files and folders. Every operation refuses rather
// than guesses when something is off, because a file manager that loses a
// file once is never trusted again: nothing is overwritten, nothing is
// deleted for good, and no name typed leaves the folder the user is in.
//
// That last rule is about names, not about links: "../x" and "/etc/x" are
// refused, but a symbolic link inside the folder is gone through, as a
// shell or an editor would ("dotfiles/nvim/init.lua", where dotfiles links
// to ~/.config). The user made the link; following it is what they asked.
package mutation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Errors a caller may want to tell apart, with errors.Is.
var (
	ErrEmptyName = errors.New("empty name")
	ErrOutside   = errors.New("name leaves the folder")
	ErrExists    = errors.New("already exists")
	ErrNotFolder = errors.New("not a folder")
)

// Created is what Create made.
type Created struct {
	// Path is the new file or folder.
	Path string
	// Top is the entry of dir that holds it: Path itself, or for
	// "a/b/c.txt" the folder "a". It is what a listing of dir can select.
	Top string
}

// Create makes the file or folder name inside dir, as typed by the user:
//
//	notes.md      a file
//	drafts/       a folder: a trailing slash names a folder, as in a path
//	a/b/c.txt     a file, creating the folders a and a/b on the way
//
// It never overwrites: if name exists the result is ErrExists and the disk
// is unchanged. Names that would land outside dir ("../x", "/etc/x") are
// refused with ErrOutside.
func Create(dir, name string) (Created, error) {
	name = strings.TrimSpace(name)
	c, err := resolve(dir, name)
	if err == nil {
		if strings.HasSuffix(name, "/") {
			err = makeDir(c.Path)
		} else {
			err = makeFile(c.Path)
		}
	}
	if err != nil {
		return Created{}, fmt.Errorf("create %q: %w", name, err)
	}
	return c, nil
}

// resolve turns a typed name into the paths it would create, refusing
// names that are empty or would leave dir. It only looks at the text; the
// disk is not touched.
func resolve(dir, name string) (Created, error) {
	clean := filepath.Clean(name) // "a//b/./c/" is "a/b/c"
	switch {
	case name == "" || clean == "." || clean == string(filepath.Separator):
		return Created{}, ErrEmptyName
	case !filepath.IsLocal(clean): // "../x", "/etc/x", and on Windows "NUL"
		return Created{}, ErrOutside
	}
	first, _, _ := strings.Cut(clean, string(filepath.Separator))
	return Created{Path: filepath.Join(dir, clean), Top: filepath.Join(dir, first)}, nil
}

// makeDir creates the folder path and any missing parents.
func makeDir(path string) error {
	// MkdirAll succeeds on a folder that already exists; the user asked
	// for a new one, so say it is there instead of pretending.
	if _, err := os.Lstat(path); err == nil {
		return ErrExists
	}
	return os.MkdirAll(path, 0o755) //nolint:gosec // the user's own folder: their umask decides
}

// makeFile creates an empty file at path, and the folders above it.
func makeFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // as in makeDir
		return err
	}
	// O_EXCL makes "never overwrite" atomic: the kernel refuses if the
	// file exists, even one that appeared a moment ago. A check followed by
	// a plain create would leave a gap in which a file could be truncated.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) //nolint:gosec // the user's own file: their umask decides
	if errors.Is(err, fs.ErrExist) {
		return ErrExists
	}
	if err != nil {
		return err
	}
	return f.Close()
}
