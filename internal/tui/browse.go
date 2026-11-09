package tui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// Browsing works like Finder: an empty query shows the contents of cwd,
// enter goes into a folder, backspace on an empty query goes back out.
// Search follows: typing searches below cwd, so the folder the user is in
// is always the scope.
//
// The listing is read from disk, not from the index (see indexer.List), in
// a command like everything else that touches the disk.

// habitsShown is how many habits are offered above a folder's contents:
// enough for the few places that matter, few enough not to push the folder
// itself off the screen.
const habitsShown = 5

// loadListing reads dir in the background, along with the habits below it
// that still exist. habits come from the history, in memory; checking that
// they exist takes the disk, so it happens here and not in Update. A place
// that was deleted or moved simply is not offered.
func loadListing(dir string, habits []string) tea.Cmd {
	return func() tea.Msg {
		es, err := indexer.List(dir)
		msg := listMsg{dir: dir, entries: es, err: err}
		for _, p := range habits {
			if info, err := os.Stat(p); err == nil {
				msg.habits = append(msg.habits, indexer.NewEntry(p, info.Size(), info.ModTime().UnixNano(), info.IsDir()))
			}
		}
		return msg
	}
}

// load reads dir with the habits below it.
func (m *Model) load(dir string) tea.Cmd {
	var habits []string
	if m.hist != nil {
		habits = m.hist.Top(dir, habitsShown, time.Now())
	}
	return loadListing(dir, habits)
}

// listed stores a directory listing, unless the user has moved on since it
// was requested.
func (m *Model) listed(msg *listMsg) {
	if msg.dir != m.cwd {
		return
	}
	m.listErr = msg.err
	m.habits = len(msg.habits)
	// Habits first, then the folder's contents: one list, so moving,
	// Enter and Ctrl+E work on habit rows exactly as on any other.
	m.listing = make([]indexer.Result, 0, len(msg.habits)+len(msg.entries))
	for _, e := range msg.habits {
		m.listing = append(m.listing, indexer.Result{Entry: e})
	}
	for _, e := range msg.entries {
		m.listing = append(m.listing, indexer.Result{Entry: e})
	}
	if m.query == "" {
		m.showListing()
	}
}

// showListing puts the listing on screen, selecting the folder just come
// up from, if any: going back out lands where you went in, as in Finder.
func (m *Model) showListing() {
	m.results, m.total = m.listing, len(m.listing)
	m.cursor, m.offset = 0, 0
	if m.listing == nil {
		return // not read yet; keep reveal for when it is
	}
	// The folder just left is looked for among the contents, not the
	// habits above them: that is where it sits in the folder.
	for i := m.habits; i < len(m.listing); i++ {
		if m.listing[i].Entry.Path == m.reveal {
			m.cursor = i
			break
		}
	}
	m.reveal = ""
	m.scrollToCursor()
}

// activate is enter on the selected row: go into a folder, open a file.
func (m *Model) activate() tea.Cmd {
	if len(m.results) == 0 {
		return nil
	}
	e := &m.results[m.cursor].Entry
	save := m.learn(e.Path)
	if !e.IsDir {
		return tea.Batch(save, m.openSelected())
	}
	dir, err := realDir(e.Path)
	if err != nil {
		m.setNotice(err.Error(), true)
		return save
	}
	return tea.Batch(save, m.chdir(dir, ""))
}

// realDir returns where entering the folder at path should lead: path
// itself, or, if path is a link, the folder it points to, as "cd -P" does.
// The crawl never follows links, so files behind one are indexed under the
// real path, if at all; under the link's path, search would find nothing.
//
// Only a link is resolved. Resolving every folder would also rewrite links
// above it ("/tmp" is "/private/tmp" on macOS) and move the user out of
// the indexed tree for nothing. An lstat or two: microseconds on a local
// disk, cheap enough not to need a command.
func realDir(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return path, nil // not a link; a vanished folder is reported by its listing
	}
	return filepath.EvalSymlinks(path)
}

// up goes to the parent of cwd and selects the folder it came from.
func (m *Model) up() tea.Cmd {
	parent := filepath.Dir(m.cwd)
	if parent == m.cwd {
		m.setNotice("already at the top", false)
		return nil
	}
	return m.chdir(parent, m.cwd)
}

// chdir makes dir the current folder: the query is cleared, the listing
// reloaded, and search narrowed to dir. Going above the indexed root
// starts a new crawl from dir, because nothing above root is indexed.
func (m *Model) chdir(dir, reveal string) tea.Cmd {
	m.cwd, m.reveal = dir, reveal
	m.listing, m.listErr = nil, nil
	m.query, m.qpos = "", 0 // the cursor must never point past the query
	m.search()              // cancels any search in flight and shows the (empty) listing

	cmds := []tea.Cmd{m.load(dir)}
	if !within(dir, m.root) {
		m.root, m.ix = dir, &indexer.Index{}
		cmds = append(cmds, m.startCrawl())
	}
	return tea.Batch(cmds...)
}

// within reports whether path is dir or lies below it. Both are absolute
// and cleaned.
func within(path, dir string) bool {
	if path == dir || dir == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
