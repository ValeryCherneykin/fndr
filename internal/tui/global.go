package tui

import (
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
	var crawl tea.Cmd
	switch {
	case m.globalOn:
		m.globalOn = false
	case m.home == "":
		m.setNotice("no home folder to search", true)
		return nil
	default:
		m.globalOn = true
		if m.global == nil {
			m.global = &scope{root: m.home, ix: &indexer.Index{}}
			crawl = m.startCrawl(m.global)
		}
	}
	m.cursor, m.offset, m.picked = 0, 0, ""
	return tea.Batch(crawl, m.search())
}

// updateIndexes tells every scope about entries changed on the disk, so
// both searches find a renamed file under its new name.
func (m *Model) updateIndexes(remove []string, add []indexer.Entry) {
	for _, s := range []*scope{m.local, m.global} {
		if s != nil {
			s.ix.Update(remove, add)
		}
	}
}
