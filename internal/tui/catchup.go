package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// What other programs change on the disk while fndr is open (a file saved
// in nvim, a download finished) reaches the index by catching up: the
// same refresh as at start, which reads only the folders whose time
// changed (indexer.Refresh), in the background, with no "indexing" shown.
//
// Watching the whole home through the kernel would take FSEvents (cgo) on
// macOS or a descriptor per file with kqueue, which a large home runs out
// of. A catch-up when the user comes back is enough: the terminal says
// when fndr's window or tmux pane is focused again (focus reporting; tmux
// needs focus-events on, as it has by default in many setups). Where it
// does not, the first key after a minute idle does the same.

const (
	// catchUpEvery is the least time between two catch-ups: focus can go
	// back and forth between panes in a second, and a catch-up of a home
	// is some 50-100 ms of work each time.
	catchUpEvery = 5 * time.Second

	// catchUpIdle is how long without a key makes the next key catch up,
	// for terminals that do not report focus.
	catchUpIdle = time.Minute
)

// catchUp starts a catch-up of every index not crawling already, and reads
// the listing again, unless one ran less than catchUpEvery ago.
func (m *Model) catchUp(now time.Time) tea.Cmd {
	if now.Sub(m.caughtUp) < catchUpEvery {
		return nil
	}
	m.caughtUp = now
	var cmds []tea.Cmd
	for _, s := range m.scopes() {
		if s.crawling || s.ix.Len() == 0 {
			continue // a crawl under way reads the disk now anyway
		}
		s.catchUp = true
		cmds = append(cmds, m.startCrawl(s))
	}
	if m.oil == nil {
		cmds = append(cmds, m.load(m.cwd)) // the listing, read again: it comes from the disk, not the index
	}
	return tea.Batch(cmds...)
}

// keyCatchUp is catchUp for a key that comes after catchUpIdle without
// one; it notes the key's time either way.
func (m *Model) keyCatchUp(now time.Time) tea.Cmd {
	idle := !m.lastKey.IsZero() && now.Sub(m.lastKey) >= catchUpIdle
	m.lastKey = now
	if !idle {
		return nil
	}
	return m.catchUp(now)
}
