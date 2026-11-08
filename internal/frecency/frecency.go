// Package frecency remembers which files and folders the user goes to, so
// the places they actually use can be ranked first.
//
// Frecency is frequency weighted by recency, as in zoxide and Firefox: every
// visit adds one to a path's count, and the count is multiplied by a weight
// that falls as the last visit gets older. A folder visited 50 times today
// outranks one visited 80 times half a year ago.
//
// The store is a small text file, one path per line, rewritten whole on Save.
// It is meant to hold hundreds or thousands of paths, not the index of a
// disk: aging (see Visit) keeps it that size.
package frecency

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// maxTotal bounds the sum of all counts. Past it every count is scaled down
// by aging, and paths whose count falls below 1 are forgotten, so old habits
// fade and the file stays small. 10000 is zoxide's value, proven in practice.
const (
	maxTotal = 10000
	ageScale = 0.9
)

// Store is the remembered paths. It is not safe for concurrent use: the TUI
// owns it and calls it from its update loop only.
type Store struct {
	path    string
	entries map[string]*entry
	total   float64 // sum of all counts, to know when to age
	dirty   bool    // changed since loaded: Save has something to write
}

type entry struct {
	count float64 // visits, scaled down by aging
	last  time.Time
}

// DefaultPath is where the store lives: $XDG_STATE_HOME/fndr/frecency, or
// ~/.local/state/fndr/frecency. State, not config: it is data fndr gathers,
// not settings a person writes, and deleting it only makes fndr forget.
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("frecency: %w", err)
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "fndr", "frecency"), nil
}

// Open loads the store at path. A missing file is an empty store: the first
// run has nothing to remember yet. Lines that cannot be parsed are skipped
// rather than failing the whole load: a damaged history should cost a few
// entries, never the ability to start.
func Open(path string) (*Store, error) {
	s := &Store{path: path, entries: make(map[string]*entry)}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("frecency: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only: Close has nothing to report

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// count <tab> unix seconds <tab> path; the path is last, so it may
		// itself contain tabs.
		fields := strings.SplitN(sc.Text(), "\t", 3)
		if len(fields) != 3 {
			continue
		}
		count, err1 := strconv.ParseFloat(fields[0], 64)
		unix, err2 := strconv.ParseInt(fields[1], 10, 64)
		if err1 != nil || err2 != nil || count <= 0 || fields[2] == "" {
			continue
		}
		s.entries[fields[2]] = &entry{count: count, last: time.Unix(unix, 0)}
		s.total += count
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("frecency: %w", err)
	}
	return s, nil
}

// Visit records a visit to path at now.
func (s *Store) Visit(path string, now time.Time) {
	if path == "" || strings.ContainsAny(path, "\n\r") {
		return // a path the line format cannot hold; vanishingly rare
	}
	e := s.entries[path]
	if e == nil {
		e = &entry{}
		s.entries[path] = e
	}
	e.count++
	e.last = now
	s.total++
	s.dirty = true

	if s.total > maxTotal {
		s.age()
	}
}

// age scales every count down and forgets what falls below one visit.
func (s *Store) age() {
	s.total = 0
	for p, e := range s.entries {
		e.count *= ageScale
		if e.count < 1 {
			delete(s.entries, p)
			continue
		}
		s.total += e.count
	}
}

// Score is how strongly path is a habit at now: 0 for a path never visited.
func (s *Store) Score(path string, now time.Time) float64 {
	e := s.entries[path]
	if e == nil {
		return 0
	}
	return e.count * weight(now.Sub(e.last))
}

// weight is how much a visit made age ago still counts. Recent visits weigh
// more than old ones, so a new project overtakes an old one within days.
func weight(age time.Duration) float64 {
	switch {
	case age < time.Hour:
		return 4
	case age < 24*time.Hour:
		return 2
	case age < 7*24*time.Hour:
		return 0.5
	default:
		return 0.25
	}
}

// Boosts returns the ranking bonus of every remembered path at now, for
// indexer.Query.Boost. The map is new on every call and never changed
// afterwards, so searches running in parallel can read it without locks
// while the store keeps recording visits.
func (s *Store) Boosts(now time.Time) map[string]int {
	m := make(map[string]int, len(s.entries))
	for p := range s.entries {
		if b := boost(s.Score(p, now)); b > 0 {
			m[p] = b
		}
	}
	return m
}

// boost turns a frecency score into ranking points. The scale is
// logarithmic: the first visits matter most, and a path visited a thousand
// times gets about a hundred points, enough to lead among matches of
// similar quality, not to beat a clearly better match of what was typed.
func boost(score float64) int {
	return int(10 * math.Log2(1+score))
}

// minTop is the score a path needs to be offered by Top: one visit today
// (weight 2) qualifies, one visit last month (0.25) does not. Top is a
// short list of habits, and a one-off from long ago is noise there.
const minTop = 1

// Top returns up to n remembered paths strictly below dir, the strongest
// habit first.
func (s *Store) Top(dir string, n int, now time.Time) []string {
	prefix := dir
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	var c []scored
	for p := range s.entries {
		if strings.HasPrefix(p, prefix) {
			if sc := s.Score(p, now); sc >= minTop {
				c = append(c, scored{p, sc})
			}
		}
	}
	slices.SortFunc(c, byHabit)

	out := make([]string, 0, min(n, len(c)))
	for _, x := range c[:min(n, len(c))] {
		out = append(out, x.path)
	}
	return out
}

type scored struct {
	path  string
	score float64
}

// byHabit orders paths for Top: the highest score first, and on a tie by
// path, so the list does not reshuffle between runs (map order is random).
func byHabit(a, b scored) int {
	// b before a: the higher score sorts first.
	if c := cmp.Compare(b.score, a.score); c != 0 {
		return c
	}
	return strings.Compare(a.path, b.path)
}

// Len is the number of remembered paths.
func (s *Store) Len() int { return len(s.entries) }

// Save writes the store back if it changed. The file is written to a
// temporary name and renamed over the old one: a rename within a directory
// is atomic, so a crash mid-write leaves the old history intact rather than
// half a file.
func (s *Store) Save() error {
	if !s.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("frecency: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".frecency-*")
	if err != nil {
		return fmt.Errorf("frecency: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone after the rename; cleanup only on failure

	w := bufio.NewWriter(tmp)
	for p, e := range s.entries {
		// A bufio.Writer remembers its first error and returns it from
		// Flush, checked below, so one check covers every line.
		fmt.Fprintf(w, "%s\t%d\t%s\n", strconv.FormatFloat(e.count, 'f', -1, 64), e.last.Unix(), p) //nolint:errcheck // see above
	}
	if err := errors.Join(w.Flush(), tmp.Close()); err != nil {
		return fmt.Errorf("frecency: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("frecency: %w", err)
	}
	s.dirty = false
	return nil
}
