package indexer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// An index is saved to disk so the next start can search at once, while a
// crawl catches up with what changed in the meantime. It is a cache: lost
// or damaged, it costs one full crawl, never a wrong result for long.
//
// The file is a format of fndr's own, in the spirit of locate's database:
//
//	"FNDRIDX\n"                          magic
//	uvarint version                      formatVersion
//	string  root                         the folder the entries are below
//	string  rules                        Prune.Rules when it was crawled
//	uvarint count
//	count × entry, sorted by path:
//	    uvarint shared                   bytes in common with the previous path
//	    string  rest                     the path after them
//	    varint  size, varint modTime
//	    byte    flags                    1 = directory
//	uint32  CRC-32 (IEEE) of everything before, little endian
//
// where a string is a uvarint length and the bytes. Sorted, neighbours in
// a home share most of their path (~/dev/fndr/internal/tui/a.go, …/b.go),
// so storing only what differs ("front coding") keeps the file a fraction
// of the paths' size. Sorting also lets the catch-up find a folder's
// entries by binary search.
//
// Load trusts nothing in the file: it may be cut short by a crash, damaged,
// written by another version, or not ours at all. Anything off is
// ErrBadSaved, and the caller crawls instead.

// formatVersion changes whenever the layout does; files of other versions
// are refused, not converted: a crawl rebuilds them.
const formatVersion = 1

const savedMagic = "FNDRIDX\n"

// ErrBadSaved means a saved index cannot be used: damaged, cut short, of
// another version, or not an index at all.
var ErrBadSaved = errors.New("saved index unusable")

// Saved is an index as written to disk.
type Saved struct {
	// Root is the folder the entries are below, absolute and cleaned.
	Root string

	// Rules is what the crawl left out (Prune.Rules). The caller compares
	// it with the rules of now: changed, the saved entries may miss what is
	// no longer pruned, and the index must be crawled again.
	Rules string

	// Entries are below Root, in no particular order: Save sorts a copy.
	Entries []Entry
}

// Save writes s to path, replacing what was there only once all of it is
// written: a temporary file renamed over the old one, so a crash leaves the
// old index or the new, never half of one. The file is readable by the
// user only: it lists the name of every file in the tree.
func Save(path string, s Saved) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("save index: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".index-*") // created 0600
	if err != nil {
		return fmt.Errorf("save index: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone after the rename; cleanup only on failure

	_, err = tmp.Write(encode(s))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("save index: %w", err)
	}
	return nil
}

// Load reads an index Save wrote. A file that is not one, whole and of
// this version, is ErrBadSaved; a missing one is fs.ErrNotExist.
func Load(path string) (Saved, error) {
	data, err := os.ReadFile(path) //nolint:gosec // our own cache file
	if err != nil {
		return Saved{}, fmt.Errorf("load index: %w", err)
	}
	s, err := decode(data)
	if err != nil {
		return Saved{}, fmt.Errorf("load index %s: %w", path, err)
	}
	return s, nil
}

// encode lays s out as the file holds it (see above).
func encode(s Saved) []byte {
	entries := slices.Clone(s.Entries) // sorted for front coding; the caller's order stays
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })

	// Room for the paths whole is more than front coding needs: one
	// allocation, never grown.
	size := 64 + len(s.Root) + len(s.Rules)
	for i := range entries {
		size += len(entries[i].Path) + 3*binary.MaxVarintLen64
	}
	b := make([]byte, 0, size)

	b = append(b, savedMagic...)
	b = binary.AppendUvarint(b, formatVersion)
	b = appendString(b, s.Root)
	b = appendString(b, s.Rules)
	b = binary.AppendUvarint(b, uint64(len(entries)))
	prev := ""
	for i := range entries {
		e := &entries[i]
		shared := commonPrefix(prev, e.Path)
		b = binary.AppendUvarint(b, uint64(shared)) //nolint:gosec // a length: never negative
		b = appendString(b, e.Path[shared:])
		b = binary.AppendVarint(b, e.Size)
		b = binary.AppendVarint(b, e.ModTime)
		var flags byte
		if e.IsDir {
			flags = 1
		}
		b = append(b, flags)
		prev = e.Path
	}
	return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
}

func appendString(b []byte, s string) []byte {
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

func commonPrefix(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// decode reads what encode wrote, checking everything a search relies on:
// every path is absolute, below the root and cleaned, and they come sorted
// and unique.
func decode(data []byte) (Saved, error) {
	if len(data) < len(savedMagic)+4 || string(data[:len(savedMagic)]) != savedMagic {
		return Saved{}, fmt.Errorf("%w: not an index", ErrBadSaved)
	}
	body := data[:len(data)-4]
	if crc32.ChecksumIEEE(body) != binary.LittleEndian.Uint32(data[len(data)-4:]) {
		return Saved{}, fmt.Errorf("%w: damaged", ErrBadSaved)
	}
	r := reader{b: body[len(savedMagic):]}
	if v := r.uvarint(); v != formatVersion {
		return Saved{}, fmt.Errorf("%w: version %d, want %d", ErrBadSaved, v, formatVersion)
	}
	s := Saved{Root: string(r.bytes()), Rules: string(r.bytes())}
	count := r.uvarint()
	if r.err != nil || !filepath.IsAbs(s.Root) || filepath.Clean(s.Root) != s.Root {
		return Saved{}, fmt.Errorf("%w: bad header", ErrBadSaved)
	}
	// Each entry takes at least 5 bytes: a count past that is a lie, and
	// must not size an allocation.
	if count > uint64(len(r.b))/5 {
		return Saved{}, fmt.Errorf("%w: bad count", ErrBadSaved)
	}

	// The paths are rebuilt into one string, which every Entry.Path then
	// slices: one allocation for all the paths instead of one per entry.
	// The price: the whole string stays alive while any entry of it is.
	//
	// A first pass reads the shape of the entries to learn how long that
	// string is, so the second writes it into a buffer of the right size:
	// grown as it went, the buffer would be copied at every doubling, and
	// on a home that was most of the time and three quarters of the memory
	// a load took.
	entries := r // a copy: the second pass reads the same bytes again
	total, prevLen := 0, 0
	for i := range count {
		shared, rest := r.uvarint(), r.bytes()
		r.varint()
		r.varint()
		if flags := r.byte(); r.err != nil || shared > uint64(prevLen) || flags > 1 {
			return Saved{}, fmt.Errorf("%w: bad entry %d", ErrBadSaved, i)
		}
		prevLen = int(shared) + len(rest) //nolint:gosec // shared <= prevLen, an int, checked just above
		total += prevLen
	}
	if len(r.b) != 0 {
		return Saved{}, fmt.Errorf("%w: trailing bytes", ErrBadSaved)
	}

	under := s.Root + string(filepath.Separator)
	if s.Root == string(filepath.Separator) {
		under = s.Root
	}
	var b strings.Builder
	b.Grow(total)
	s.Entries = make([]Entry, count)
	prev := ""
	for i := range s.Entries {
		shared, rest := entries.uvarint(), entries.bytes()
		size, mod, flags := entries.varint(), entries.varint(), entries.byte()
		start := b.Len()
		b.WriteString(prev[:shared])
		b.Write(rest)
		// String does not copy, and the buffer, sized above, never moves:
		// earlier paths stay valid as later ones are written after them.
		p := b.String()[start:]
		// Only what differs from prev needs checking: the parts before it
		// are prev's, checked already. From the separator before the
		// first new byte, so a part that the new bytes finish is whole.
		from := max(strings.LastIndexByte(p[:shared], filepath.Separator), len(under)-1)
		if !strings.HasPrefix(p, under) || len(p) == len(under) || !clean(p[from:]) || (i > 0 && p <= prev) {
			return Saved{}, fmt.Errorf("%w: bad path at entry %d", ErrBadSaved, i)
		}
		s.Entries[i] = NewEntry(p, size, mod, flags == 1)
		prev = p
	}
	return s, nil
}

// clean reports whether p, a path starting with a separator, is as
// filepath.Clean leaves it: no empty, "." or ".." part, no separator at the
// end. Checked by hand: Clean itself would allocate for every entry.
func clean(p string) bool {
	part := 0 // length of the part being read
	dots := 0 // how many of its bytes are dots
	for i := 1; i <= len(p); i++ {
		if i < len(p) && p[i] != filepath.Separator {
			part++
			if p[i] == '.' {
				dots++
			}
			continue
		}
		if part == 0 || (part == dots && part <= 2) {
			return false // "//", a trailing "/", "/./" or "/../"
		}
		part, dots = 0, 0
	}
	return true
}

// reader takes values off the front of b. The first failure sticks in err
// and every read after it returns zero, so a run of reads needs one check.
type reader struct {
	b   []byte
	err error
}

func (r *reader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.err = ErrBadSaved
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *reader) varint() int64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Varint(r.b)
	if n <= 0 {
		r.err = ErrBadSaved
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *reader) bytes() []byte {
	n := r.uvarint()
	if r.err != nil || n > uint64(len(r.b)) {
		r.err = ErrBadSaved
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) byte() byte {
	if r.err != nil || len(r.b) == 0 {
		r.err = ErrBadSaved
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

// DefaultSavedPath is where the index of the home is kept:
// $XDG_CACHE_HOME/fndr/index, or ~/.cache/fndr/index. A cache, as the
// name says: deleting it costs one crawl.
func DefaultSavedPath() (string, error) {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" || !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("index path: %w", err)
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "fndr", "index"), nil
}

// Under returns the entries of sorted, sorted by path as Load returns
// them, that are below dir: a part of sorted, not a copy, found by binary
// search in microseconds where a scan of a home would take milliseconds.
//
// Below dir means starting with dir and a separator, and sorted by bytes
// those paths are all together: from dir+"/" up to dir+"0", "0" being
// the byte after "/".
func Under(sorted []Entry, dir string) []Entry {
	lo := dir + string(filepath.Separator)
	if dir == string(filepath.Separator) {
		lo = dir
	}
	hi := lo[:len(lo)-1] + string(rune(filepath.Separator+1))
	cmp := func(e Entry, p string) int { return strings.Compare(e.Path, p) }
	i, _ := slices.BinarySearchFunc(sorted, lo, cmp)
	j, _ := slices.BinarySearchFunc(sorted, hi, cmp)
	return sorted[i:j]
}
