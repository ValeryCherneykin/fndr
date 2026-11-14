package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ValeryCherneykin/fndr/internal/config"
)

// useFolders adds the config's skip and include lines to what the crawl
// leaves out (indexer.Prune), and says what it could not use. The rules
// are part of what a saved index was crawled under: changed, the next
// start crawls the home whole once.
//
// An include must be reachable: in ~/.local/share/x, ~/.local is never
// gone into, and the line would silently do nothing. So every include is
// added first, for one line to open the way for another, then each is
// checked.
func (m *Model) useFolders(skip, include []config.Folder) []error {
	var errs []error
	skipped := map[string]int{}
	for _, f := range skip {
		path, err := m.folderPath(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		m.prune.Skip(path)
		skipped[path] = f.Line
	}
	var added []config.Folder
	var paths []string
	for _, f := range include {
		path, err := m.folderPath(f)
		if err == nil {
			if line, ok := skipped[path]; ok {
				err = &config.Error{Line: f.Line, Err: fmt.Errorf("%s is skipped on line %d: which is it?", f.Path, line)}
			}
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		m.prune.Include(path)
		added, paths = append(added, f), append(paths, path)
	}
	for i, f := range added {
		path := paths[i]
		if under(path, m.home) && !m.prune.Reaches(m.home, filepath.Dir(path)) {
			errs = append(errs, &config.Error{Line: f.Line, Err: fmt.Errorf(
				"%s is inside a folder fndr does not go into: include that first", f.Path)})
		}
	}
	// In the file's order, as the user reads it: the checks above run in
	// two passes.
	slices.SortStableFunc(errs, func(a, b error) int { return line(a) - line(b) })
	return errs
}

// line is the config line an error is on, 0 for none.
func line(err error) int {
	var e *config.Error
	if errors.As(err, &e) {
		return e.Line
	}
	return 0
}

// folderPath is the folder of a skip or include line, whole: "~/" read as
// the home.
func (m *Model) folderPath(f config.Folder) (string, error) {
	rest, home := strings.CutPrefix(f.Path, "~/")
	if !home {
		return filepath.Clean(f.Path), nil
	}
	if m.home == "" {
		return "", &config.Error{Line: f.Line, Err: fmt.Errorf("%s: no home to read ~ as", f.Path)}
	}
	return filepath.Join(m.home, rest), nil
}

// under reports whether path is dir or below it.
func under(path, dir string) bool {
	return dir != "" && (path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)))
}
