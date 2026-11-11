package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/filter"
)

// Tags narrow the search (package filter). There is no field for them and
// no mode: a tag is typed in the query line like anything else, and ctrl+t
// makes what is typed a tag, drawn as a chip before the query. So the user
// starts typing first and decides what it is after, and sees the tag while
// typing it. Several tags must all match; the same tag again takes it off.
//
// Tags stay through Tab and through folders entered: they are the cards on
// the desk, the query is what is looked for among them. Backspace on an
// empty query and Esc take the last one off.

// browsing reports whether the list shows the folder rather than a
// search: nothing typed and no tags. With a tag alone, it is a search of
// everything below the folder that the tag lets through.
func (m *Model) browsing() bool { return m.query == "" && len(m.tags) == 0 }

// addTag makes the query a tag, or takes that tag off if it is on.
func (m *Model) addTag() tea.Cmd {
	text := strings.TrimSpace(m.query)
	if text == "" {
		m.setNotice("type a tag, then ctrl+t: .go  dirs  image  size>1mb  .go|dirs", false)
		return nil
	}
	t, err := filter.Parse(text)
	if err != nil {
		m.setNotice("tag "+safeText(text)+": "+err.Error(), true)
		return nil
	}
	m.tags = m.tags.Toggle(t)
	m.query, m.qpos = "", 0 // the query was the tag
	return m.tagsChanged()
}

// popTag takes the last tag off.
func (m *Model) popTag() tea.Cmd {
	// Clip: a slice shorter than its array would let a later append write
	// over the tag taken off, which a search in flight may still read.
	m.tags = slices.Clip(m.tags[:len(m.tags)-1])
	return m.tagsChanged()
}

// tagsChanged searches again from the top.
func (m *Model) tagsChanged() tea.Cmd {
	m.cursor, m.offset, m.picked = 0, 0, ""
	return m.search()
}

// viewChips renders the tags as chips, each followed by a space, for the
// query line.
func (m *Model) viewChips() string {
	var b strings.Builder
	for _, t := range m.tags {
		b.WriteString(stChip.Render(" " + safeText(t.String()) + " "))
		b.WriteByte(' ')
	}
	return b.String()
}
