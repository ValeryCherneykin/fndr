// Package frecency remembers which files and folders the user goes to, so
// the places they actually use can be ranked first.
//
// Frecency is frequency weighted by recency, as in zoxide and Firefox: every
// visit adds one to a path's count, and the count is multiplied by a weight
// that falls as the last visit gets older. A folder visited 50 times today
// outranks one visited 80 times half a year ago.
//
// The store is a small text file, one path per line, rewritten whole on
// every save. It is meant to hold hundreds or thousands of paths, not the
// index of a disk: aging (see merge) keeps it that size.
//
// Several fndr can run at once, one per tmux pane, all sharing the file.
// If each wrote back what it had read plus its own visits, the last to save
// would erase the visits of the others: a lost update. So a store remembers
// only what this session added (pending), and saving is read, add, write,
// under a lock on the file: visits from every pane add up.
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
	"sync"
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
// owns it and calls it from its update loop only. The one part that runs
// elsewhere, Flush.Write, touches nothing of the Store.
type Store struct {
	path    string
	entries map[string]*entry // what ranks: the file as last read, plus pending
	pending map[string]*entry // visits of this session not yet in the file
	flight  *Flush            // the save running, if any; see Flush
}

type entry struct {
	count float64 // visits, scaled down by aging
	last  time.Time
}

// add counts the visits of o into e: counts add up, the latest visit wins.
func (e *entry) add(o *entry) {
	e.count += o.count
	if o.last.After(e.last) {
		e.last = o.last
	}
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
	entries, err := read(path)
	if err != nil {
		return nil, fmt.Errorf("frecency: %w", err)
	}
	return &Store{path: path, entries: entries, pending: map[string]*entry{}}, nil
}

// read parses the file at path; a missing file reads as empty.
func read(path string) (map[string]*entry, error) {
	entries := map[string]*entry{}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return nil, err
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
		entries[fields[2]] = &entry{count: count, last: time.Unix(unix, 0)}
	}
	return entries, sc.Err()
}

// Visit records a visit to path at now. It ranks at once; it reaches the
// file with the next save.
func (s *Store) Visit(path string, now time.Time) {
	if path == "" || strings.ContainsAny(path, "\n\r") {
		return // a path the line format cannot hold; vanishingly rare
	}
	visit := &entry{count: 1, last: now}
	addTo(s.entries, path, visit)
	addTo(s.pending, path, visit)
}

// addTo counts visits of path into m.
func addTo(m map[string]*entry, path string, visits *entry) {
	if e := m[path]; e != nil {
		e.add(visits)
		return
	}
	cp := *visits
	m[path] = &cp
}

// merge adds pending into entries and ages them: it is what a save writes.
// Aging scales every count down once their sum passes maxTotal, and forgets
// what falls below one visit.
func merge(entries, pending map[string]*entry) {
	for p, e := range pending {
		addTo(entries, p, e)
	}
	for {
		total := 0.0
		for _, e := range entries {
			total += e.count
		}
		if total <= maxTotal {
			return
		}
		for p, e := range entries {
			e.count *= ageScale
			if e.count < 1 {
				delete(entries, p)
			}
		}
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

// Saving runs in two halves, so the interface never waits for the disk:
//
//	f := s.Flush()   // UI goroutine: hand the pending visits over
//	f.Write()        // any goroutine: lock, read, add, write, unlock
//	s.Saved(f)       // UI goroutine: take in what the file now holds
//
// The file is read again under the lock rather than trusted from memory:
// another fndr may have saved since. After Saved, the store ranks with the
// visits of every pane, not only its own.

// Flush is a save on its way to the file.
type Flush struct {
	path    string
	pending map[string]*entry
	result  map[string]*entry // the file as written, once Write succeeds
	err     error             // what Write returned

	// once makes Write run a single time, whoever calls it first: the
	// command it was handed to, or Save at the end. A Flush whose command
	// never ran (the program quit first) is then written by Save rather
	// than waited for forever, and one already running is waited for.
	once sync.Once
}

// Flush hands the pending visits to a new save, or returns nil when there
// is nothing to save or a save is already running: its Saved will see what
// was added since, and the caller saves again then.
func (s *Store) Flush() *Flush {
	if len(s.pending) == 0 || s.flight != nil {
		return nil
	}
	f := &Flush{path: s.path, pending: s.pending}
	s.pending, s.flight = map[string]*entry{}, f
	return f
}

// Write adds the visits to the file. It is safe on any goroutine: it reads
// only the Flush, and the lock orders it with every other fndr's Write.
// Calls after the first wait for it and return its error.
func (f *Flush) Write() error {
	f.once.Do(func() {
		f.result, f.err = f.write()
		if f.err != nil {
			f.err = fmt.Errorf("frecency: %w", f.err)
		}
	})
	return f.err
}

func (f *Flush) write() (map[string]*entry, error) {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return nil, err
	}
	unlock, err := lock(f.path + ".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()

	entries, err := read(f.path)
	if err != nil {
		return nil, err
	}
	merge(entries, f.pending)
	return entries, write(f.path, entries)
}

// Saved takes in a finished save and returns its error. On success the
// store ranks by the file as written, which holds the other panes' visits
// too, plus whatever was visited meanwhile. On failure the visits go back
// to pending, for the next save to try again: none is lost.
func (s *Store) Saved(f *Flush) error {
	if f == nil {
		return nil
	}
	s.flight = nil
	if f.err != nil {
		for p, e := range f.pending {
			addTo(s.pending, p, e)
		}
		return f.err
	}
	s.entries = f.result
	for p, e := range s.pending {
		addTo(s.entries, p, e)
	}
	return nil
}

// Pending reports whether visits are waiting for a save.
func (s *Store) Pending() bool { return len(s.pending) > 0 }

// Save writes the pending visits and waits for it: for the end of the
// program, and for tests. A save handed out earlier is finished first, so
// its visits are neither lost nor written twice: waited for if it is
// running, written here if its command never ran.
func (s *Store) Save() error {
	if f := s.flight; f != nil {
		f.Write()  //nolint:errcheck // Saved reads it
		s.Saved(f) //nolint:errcheck // a failed save's visits are pending again: retried just below
	}
	f := s.Flush()
	if f == nil {
		return nil
	}
	f.Write() //nolint:errcheck // Saved returns it
	return s.Saved(f)
}

// write replaces the file at path with entries. The lines go to a temporary
// file that is renamed over the old one: a rename within a directory is
// atomic, so a crash mid-write leaves the old history intact rather than
// half a file.
func write(path string, entries map[string]*entry) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".frecency-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone after the rename; cleanup only on failure

	w := bufio.NewWriter(tmp)
	for p, e := range entries {
		// A bufio.Writer remembers its first error and returns it from
		// Flush, checked below, so one check covers every line.
		fmt.Fprintf(w, "%s\t%d\t%s\n", strconv.FormatFloat(e.count, 'f', -1, 64), e.last.Unix(), p) //nolint:errcheck // see above
	}
	if err := errors.Join(w.Flush(), tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
