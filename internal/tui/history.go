package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/frecency"
)

// fndr learns where the user goes (package frecency) to rank search and to
// offer habits above a folder. Learning is optional: without a history
// (hist is nil) everything works, nothing is ranked by habit.

// openHistory opens the history at its usual place.
func openHistory() (*frecency.Store, error) {
	path, err := frecency.DefaultPath()
	if err != nil {
		return nil, err
	}
	return frecency.Open(path)
}

// useHistory makes fndr learn into hist and rank by it; nil turns learning
// off.
func (m *Model) useHistory(hist *frecency.Store) {
	m.hist = hist
	if hist != nil {
		m.boosts = hist.Boosts(time.Now())
	}
}

// learn records that the user went to path, and saves it at once, in the
// background: a terminal closed with fndr open kills it before any exit
// code runs, and the visit must not be lost with it.
func (m *Model) learn(path string) tea.Cmd {
	if m.hist == nil || path == "" {
		return nil
	}
	now := time.Now()
	m.hist.Visit(path, now)
	m.boosts = m.hist.Boosts(now) // a new map: searches in flight keep the old one
	return m.saveHistory()
}

// historySavedMsg reports a background save of the history.
type historySavedMsg struct{ f *frecency.Flush }

// saveHistory writes the pending visits off the UI goroutine. With a save
// already running it does nothing; that save's reply starts the next one.
func (m *Model) saveHistory() tea.Cmd {
	f := m.hist.Flush()
	if f == nil {
		return nil
	}
	return func() tea.Msg {
		f.Write() //nolint:errcheck // Saved reports it, back on the UI goroutine
		return historySavedMsg{f}
	}
}

// historySaved takes in a finished save. The history now holds the visits
// of every fndr sharing it, so the ranking is refreshed.
func (m *Model) historySaved(msg historySavedMsg) tea.Cmd {
	if err := m.hist.Saved(msg.f); err != nil {
		m.setNotice("history not saved: "+err.Error(), true)
		return nil // kept as pending: the next visit tries again
	}
	m.boosts = m.hist.Boosts(time.Now())
	if m.hist.Pending() {
		return m.saveHistory() // visited while that save ran
	}
	return nil
}

// SaveHistory writes what is still unsaved and waits for it. The program
// calls it once, after the interface has closed.
func (m *Model) SaveHistory() error {
	if m.hist == nil {
		return nil
	}
	return m.hist.Save()
}
