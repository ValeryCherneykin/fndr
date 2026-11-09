package indexer

import (
	"path/filepath"
	"strings"
)

// Entry is a single file or directory in the index.
//
// Entries are stored by value in one large slice, so the struct is kept small
// and its fields are ordered from largest to smallest to avoid padding holes:
// on 64-bit platforms it is 40 bytes. Adding a field here costs that many
// bytes times the number of files indexed; entry_test.go guards the size.
type Entry struct {
	// Path is the absolute, cleaned path (see filepath.Clean), so it never
	// ends with a separator except for the root itself. It is exported for
	// reading; changing it would leave nameOff pointing into the old path,
	// so build a new Entry with NewEntry instead.
	Path string

	// Size is the size in bytes as reported by the file system. For a
	// directory this is the size of the directory record, not of its
	// contents.
	Size int64

	// ModTime is the modification time in Unix nanoseconds. It is an int64
	// rather than a time.Time because time.Time is 24 bytes and holds a
	// pointer, which the garbage collector would have to visit for every
	// entry on every cycle.
	ModTime int64

	// nameOff is the byte offset in Path where the base name starts. It is
	// computed once by NewEntry and unexported so that callers cannot break
	// the invariant; Name is the only way to read it.
	//
	// uint32 rather than uint16: the struct is padded to a multiple of 8
	// anyway, so uint16 would save nothing and only add an overflow case.
	nameOff uint32

	// IsDir reports whether the entry is a directory.
	IsDir bool
}

// NewEntry returns an Entry for the given absolute, cleaned path.
//
// It locates the base name once, at indexing time, so that Name can be called
// on every entry for every keystroke without scanning the path again.
func NewEntry(path string, size, modTime int64, isDir bool) Entry {
	return Entry{
		Path:    path,
		Size:    size,
		ModTime: modTime,
		nameOff: nameOffset(path),
		IsDir:   isDir,
	}
}

// Name returns the last element of the path, like filepath.Base, without
// allocating: the result shares memory with Path.
//
// The root directory is its own name ("/"), matching filepath.Base. A zero
// Entry built without NewEntry has no recorded offset, so Name returns the
// whole path rather than garbage.
func (e Entry) Name() string {
	return e.Path[e.nameOff:]
}

// nameOffset returns the byte offset where the base name of a cleaned path
// starts: just past the last separator.
func nameOffset(path string) uint32 {
	off := strings.LastIndexByte(path, filepath.Separator) + 1

	// In a cleaned path only the root ends with a separator. An offset at the
	// very end would make Name return "", so the root keeps the whole path
	// as its name instead.
	if off == len(path) {
		return 0
	}

	// A path longer than 4 GiB cannot come from a real file system, so the
	// conversion cannot overflow in practice.
	return uint32(off) //nolint:gosec // see comment above
}
