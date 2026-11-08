package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/preview"
)

// The preview pane follows the selection without ever slowing it down.
//
// Reading a file takes the disk, so it never happens in Update: a selection
// change starts a timer, and only if the selection is still there when it
// fires is the preview built, in a command, off the UI goroutine. Holding ↓
// repeats the key every 30-50 ms, faster than the delay, so rows flown past
// cost nothing. 60 ms is still well under the ~100 ms a person notices.
//
// Built previews are cached by path, so going back to a row is instant.
const (
	// previewMinWidth is the narrowest terminal that gets a preview pane.
	// Below it, halves would be too thin to read: the list stays whole even
	// with the preview switched on.
	previewMinWidth = 100

	previewDelay = 60 * time.Millisecond

	// paneMaxLines is how many lines of text a preview keeps: more than any
	// terminal is tall, so the cache stays valid when the window grows.
	paneMaxLines = 200

	// paneCacheMax bounds the cache. When full it is simply dropped: an
	// eviction policy is not worth it for something rebuilt in a millisecond.
	paneCacheMax = 256
)

type (
	// paneTickMsg says the selection may have settled on path.
	paneTickMsg struct{ path string }

	// paneMsg carries a built preview.
	paneMsg struct {
		path string
		paneEntry
	}

	// paneEntry is a cached preview, or why it could not be built.
	paneEntry struct {
		p   preview.Preview
		err error
	}
)

// selectedPath is the path of the selected row, or "" when nothing is.
func (m *Model) selectedPath() string {
	if m.cursor >= len(m.results) {
		return ""
	}
	return m.results[m.cursor].Entry.Path
}

// wantPreview points the pane at the current selection and, if it is new
// and not cached, starts the timer. With the pane hidden it does nothing:
// no timer, no reads.
func (m *Model) wantPreview() tea.Cmd {
	if !m.paneShown() {
		m.paneWant = "" // so showing the pane again reloads what it shows
		return nil
	}
	path := m.selectedPath()
	if path == m.paneWant {
		return nil // already on its way or shown
	}
	m.paneWant = path
	if _, ok := m.paneCache[path]; ok || path == "" {
		return nil
	}
	return tea.Tick(previewDelay, func(time.Time) tea.Msg { return paneTickMsg{path: path} })
}

// paneTick builds the preview the timer was started for, unless the
// selection has moved on since.
func (m *Model) paneTick(msg paneTickMsg) tea.Cmd {
	if msg.path != m.paneWant {
		return nil
	}
	path := msg.path
	return func() tea.Msg {
		p, err := preview.Build(path, paneMaxLines)
		return paneMsg{path: path, paneEntry: paneEntry{p, err}}
	}
}

// paneLoaded caches a built preview. It is kept even if the selection has
// moved on: it is still true for its path, and going back is likely.
func (m *Model) paneLoaded(msg *paneMsg) {
	if m.paneCache == nil || len(m.paneCache) >= paneCacheMax {
		m.paneCache = make(map[string]paneEntry)
	}
	m.paneCache[msg.path] = msg.paneEntry
}

// listWidth is how many cells of each row the result list takes: the whole
// width, or the left half when the preview pane is shown.
func (m *Model) listWidth() int {
	if m.preview && m.width >= previewMinWidth {
		return m.width / 2
	}
	return m.width
}

// paneShown reports whether the preview pane takes the right side.
func (m *Model) paneShown() bool { return m.listWidth() < m.width }

// viewHairline draws the line under the query. With the pane shown, a "┬"
// joins it to the pane's border, so the two read as one frame.
func (m *Model) viewHairline() string {
	if !m.paneShown() {
		return stHair.Render(strings.Repeat("─", m.width))
	}
	lw := m.listWidth()
	return stHair.Render(strings.Repeat("─", lw) + "┬" + strings.Repeat("─", m.width-lw-1))
}

// viewPaneLine appends line i of the preview pane, border included, after a
// list row. Without the pane it appends nothing.
func (m *Model) viewPaneLine(b *strings.Builder, i int) {
	if !m.paneShown() {
		return
	}
	_ = i // the pane's contents come with the next step
	b.WriteString(stHair.Render("│"))
	b.WriteString(strings.Repeat(" ", m.width-m.listWidth()-1))
}
