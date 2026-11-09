package indexer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// List returns the entries directly in dir, in display order (see
// listOrder). It reads the directory itself rather than the index, so it is
// exact even while a crawl is running, and works for directories the crawl
// skips, such as .git: what is not indexed is still there when the user
// goes looking.
//
// Entries that vanish between reading the directory and reading their
// details are left out, as Crawl does.
//
// Unlike Crawl, List follows a symbolic link far enough to see whether it
// points to a folder, and if so lists it as one: a link like ~/dev → /work
// is a folder to the user, to be entered, not a file to be opened. Crawl
// must not follow links (a link to a parent would loop forever); List
// looks at one level only, so it cannot loop.
func List(dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	es := make([]Entry, 0, len(des))
	for _, d := range des {
		info, err := d.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, d.Name())
		isDir := d.IsDir()
		if d.Type()&fs.ModeSymlink != 0 {
			// A broken link stays a file: opening it reports why it is broken.
			if target, err := os.Stat(path); err == nil {
				isDir = target.IsDir()
			}
		}
		es = append(es, NewEntry(path, info.Size(), info.ModTime().UnixNano(), isDir))
	}
	// Stable, on top of ReadDir's byte order: names that compare equal
	// ignoring case ("README", "readme") keep a fixed order between calls,
	// so the list never shuffles under the cursor.
	slices.SortStableFunc(es, listOrder)
	return es, nil
}

// listOrder is the display order of a directory, as in Finder: visible
// entries before hidden ones (name starting with "."); within each, folders
// before files; then by name, ignoring case.
//
// Like every comparison function for slices.SortFunc it returns a negative
// number if a goes first, positive if b goes first, and 0 if either may.
func listOrder(a, b Entry) int {
	aHidden := strings.HasPrefix(a.Name(), ".")
	bHidden := strings.HasPrefix(b.Name(), ".")

	if aHidden != bHidden {
		if aHidden {
			return 1
		}
		return -1
	}

	if a.IsDir != b.IsDir {
		if a.IsDir {
			return -1
		}
		return 1
	}

	// ToLower allocates on every comparison. Fine here: a directory holds
	// hundreds of entries, not the millions Search goes through.
	return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
}
