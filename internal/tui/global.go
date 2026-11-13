package tui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// GLOBAL search runs over the whole home instead of the folder: Tab
// switches, keeping the query, and back. The home has its own scope, so
// LOCAL keeps an index of its own folder whatever the home's crawl leaves
// out: fndr opened in ~/.config, a hidden folder, still searches it.
//
// The home is crawled the first time GLOBAL is asked for, not at start:
// whoever never presses Tab never pays for it. On the author's home that
// crawl is about 0.6 s, in the background, results arriving as it goes.

// With an empty query GLOBAL shows the places below the home the user
// goes to most, as LOCAL shows the folder: what GLOBAL is for is getting
// to them from anywhere. homeHabitsShown is a screenful.
const homeHabitsShown = 50

// homeHabitsMsg carries the places to show, those still on the disk.
type homeHabitsMsg struct{ entries []indexer.Entry }

// loadHomeHabits reads the habits again, as they are now: each Tab shows
// where the user has gone since.
func (m *Model) loadHomeHabits() tea.Cmd {
	var paths []string
	if m.hist != nil {
		paths = m.hist.Top(m.home, homeHabitsShown, time.Now())
	}
	return func() tea.Msg { return homeHabitsMsg{entries: existing(paths)} }
}

// homeHabitsLoaded stores the habits, and shows them if GLOBAL's empty
// query is still what is on screen.
func (m *Model) homeHabitsLoaded(msg *homeHabitsMsg) {
	m.homeHabits = make([]indexer.Result, 0, len(msg.entries)) // empty, not nil: read, and nothing there
	for _, e := range msg.entries {
		m.homeHabits = append(m.homeHabits, indexer.Result{Entry: e})
	}
	if m.globalOn && m.browsing() {
		m.showListing()
	}
}

// current is the scope search runs on now.
func (m *Model) current() *scope {
	if m.globalOn {
		return m.global
	}
	return m.local
}

// under is the folder search is narrowed to: the home in GLOBAL, the
// current folder in LOCAL. Result rows show folders relative to it.
func (m *Model) under() string {
	if m.globalOn {
		return m.global.root
	}
	return m.cwd
}

// accent is the colour of the scope searched (see the type).
func (m *Model) accent() *accent {
	if m.globalOn {
		return &accentGlobal
	}
	return &accentLocal
}

// toggleGlobal switches between LOCAL and GLOBAL and searches the same
// query again, from the top of the list.
func (m *Model) toggleGlobal() tea.Cmd {
	var crawl, habits tea.Cmd
	switch {
	case m.globalOn:
		m.globalOn = false
	case m.home == "":
		m.setNotice("no home folder to search", true)
		return nil
	default:
		m.globalOn = true
		switch {
		case m.global != nil:
		case m.local.root == m.home:
			m.global = m.local // LOCAL is the home already: one index, one crawl
		default:
			m.global = &scope{root: m.home, ix: &indexer.Index{}}
			crawl = m.startCrawl(m.global)
		}
		habits = m.loadHomeHabits()
	}
	m.cursor, m.offset, m.picked = 0, 0, ""
	return tea.Batch(crawl, habits, m.search())
}

// updateIndexes tells every scope about entries changed on the disk, so
// both searches find a renamed file under its new name.
func (m *Model) updateIndexes(remove []string, add []indexer.Entry) {
	for _, s := range m.scopes() {
		// Only what is below the scope's root: an edit in OIL in /tmp has
		// no place in the index of the home, and would keep it from being
		// saved in a form the next start can read.
		add := slices.DeleteFunc(slices.Clone(add), func(e indexer.Entry) bool { return !within(e.Path, s.root) })
		s.ix.Update(remove, add)
		if s.next != nil {
			s.changes = append(s.changes, change{remove, add}) // see scope.next
		}
	}
}
