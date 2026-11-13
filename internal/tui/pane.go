package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/highlight"
	"github.com/ValeryCherneykin/fndr/internal/indexer"
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

	// previewDelay is how long the selection must rest before it is read.
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

	// paneEntry is a cached preview, or why it could not be built. Text
	// is highlighted when it is read, off the UI goroutine, so drawing only
	// paints spans that are already there.
	paneEntry struct {
		p    preview.Preview
		code [][]highlight.Span // Text only: p.Lines, highlighted
		img  *paneImage         // Image only, once sent to the terminal
		err  error

		// imgFor is the room img was asked for, zero until it is. A
		// picture is asked for once per size: the pane growing (a tmux
		// zoom) asks again, the same size does not.
		imgFor box
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
	if path != m.paneWant {
		m.paneWant, m.paneScroll = path, 0 // a new file is seen from its top
		if _, ok := m.paneCache[path]; !ok && path != "" {
			return tea.Tick(previewDelay, func(time.Time) tea.Msg { return paneTickMsg{path: path} })
		}
	}
	return m.wantImage(path)
}

// wantImage asks for the picture of a cached image preview at path, fit to
// the pane as it is now, unless it is shown or on its way. It runs after
// every event, so a picture comes back by itself when it was forgotten
// (its id went to another) or no longer fits (the pane changed size).
// Until it arrives the pane shows the card.
func (m *Model) wantImage(path string) tea.Cmd {
	e, ok := m.paneCache[path]
	if !ok || e.err != nil || e.p.Kind != preview.Image || !m.graphics {
		return nil
	}
	b := m.imageBox()
	if e.imgFor == b {
		return nil
	}
	id := m.nextImageID(path)
	e.img, e.imgFor = nil, b
	m.paneCache[path] = e
	return loadImage(path, id, b, m.tmux)
}

// imageBox is the room for a picture: the pane less its border and a cell
// of margin each side.
func (m *Model) imageBox() box {
	return box{cols: m.width - m.listWidth() - 3, rows: m.listHeight()}
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
		e := paneEntry{p: p, err: err}
		if err == nil && p.Kind == preview.Text {
			e.code = highlight.Lines(path, p.Lines)
		}
		return paneMsg{path: path, paneEntry: e}
	}
}

// paneLoaded caches a built preview. It is kept even if the selection has
// moved on: it is still true for its path, and going back is likely. A
// picture, in a terminal that can show one, is then asked for by
// wantImage, which Update runs right after.
func (m *Model) paneLoaded(msg *paneMsg) {
	if m.paneCache == nil || len(m.paneCache) >= paneCacheMax {
		m.paneCache = make(map[string]paneEntry)
	}
	m.paneCache[msg.path] = msg.paneEntry
}

// imageLoaded stores a prepared picture and sends it to the terminal. A
// picture is dropped if it is no longer the one wanted: its id has since
// been handed to another path (the terminal would show the newer picture
// under it), or the pane has changed size and a new one is on its way.
func (m *Model) imageLoaded(msg *paneImageMsg) tea.Cmd {
	e, ok := m.paneCache[msg.path]
	if !ok || msg.img == nil || e.imgFor != msg.box || m.imageOwner[msg.img.id] != msg.path {
		return nil
	}
	e.img = msg.img
	m.paneCache[msg.path] = e
	return tea.Raw(msg.img.seq)
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

// writePaneLine appends line i of the pane, border included, after a list
// row. Without the pane (lines is nil) it appends nothing.
func writePaneLine(b *strings.Builder, lines []string, i int) {
	if lines == nil {
		return
	}
	b.WriteString(stHair.Render("│"))
	b.WriteString(lines[i])
}

// paneLines renders the pane as h lines of exactly its width, or returns nil
// when the pane is not shown. Content starts one cell in from the border.
//
// Until the preview arrives the pane stays blank: it takes 60 ms, and a
// "loading…" flashing on every row would be noisier than the wait.
func (m *Model) paneLines(h int) []string {
	if !m.paneShown() {
		return nil
	}
	pw := m.width - m.listWidth() - 1 // the border takes one cell
	cw := max(pw-2, 0)                // content: a cell of margin each side

	var body []string
	if e, ok := m.paneCache[m.paneWant]; ok && m.paneWant != "" {
		switch {
		case e.err != nil:
			body = []string{stPaneDim.Render(ansi.Truncate("cannot preview: "+safeText(e.err.Error()), cw, "…"))}
		case e.p.Kind == preview.Dir:
			body = paneDir(e.p.Entries[min(m.paneScroll, len(e.p.Entries)):], cw, h)
		case e.p.Kind == preview.Text:
			body = paneText(e.code[min(m.paneScroll, len(e.code)):], cw, h)
		case e.p.Kind == preview.Image && e.img != nil:
			body = e.img.lines()
		default:
			body = paneCard(&e.p)
		}
	}

	out := make([]string, h)
	for i := range out {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		out[i] = finish(" "+line, pw)
	}
	return out
}

// paneDir draws a folder's contents the way the list draws rows: icon, then
// name, hidden entries dimmed.
func paneDir(es []indexer.Entry, cw, h int) []string {
	if len(es) == 0 {
		return []string{stPaneDim.Render("empty folder")}
	}
	es = es[:min(len(es), h)]
	out := make([]string, len(es))
	for i := range es {
		name := safeText(es[i].Name())
		st := pick(strings.HasPrefix(name, "."), &stNameHidden, &stName)
		out[i] = iconFor(name, es[i].IsDir) + " " + st.Render(ansi.Truncate(name, max(cw-2, 0), "…"))
	}
	return out
}

// paneTabWidth is how many spaces a tab becomes. Tabs must be expanded:
// printed raw, the terminal moves to its own tab stops, measured from the
// screen's edge rather than the pane's, and the layout breaks.
const paneTabWidth = 4

// paneText draws the first h lines of highlighted text, cut at the pane's
// edge rather than wrapped, the way an editor shows a long line.
func paneText(code [][]highlight.Span, cw, h int) []string {
	code = code[:min(len(code), h)] // only what is on screen gets styled
	tab := strings.Repeat(" ", paneTabWidth)
	out := make([]string, len(code))
	for i, spans := range code {
		var b strings.Builder
		room := cw
		for _, sp := range spans {
			text := strings.ReplaceAll(sp.Text, "\t", tab)
			if w := ansi.StringWidth(text); w > room {
				text = ansi.Truncate(text, room, "")
			}
			b.WriteString(stCode[sp.Kind].Render(text))
			if room -= ansi.StringWidth(text); room <= 0 {
				break
			}
		}
		out[i] = b.String()
	}
	return out
}

// paneCard draws the facts about a file with nothing readable to show.
func paneCard(p *preview.Preview) []string {
	return []string{
		"",
		stPaneTitle.Render(p.Type),
		stPaneDim.Render(humanSize(p.Size)),
		stPaneDim.Render(p.ModTime.Format("2 Jan 2006 15:04")),
	}
}

// humanSize formats a file size the way Finder does, in powers of 1000:
// 999 B, 4.2 KB, 1.3 MB, 5 GB.
func humanSize(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n) + " B"
	case n < 1_000_000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1e3)) + " KB"
	case n < 1_000_000_000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1e6)) + " MB"
	default:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1e9)) + " GB"
	}
}

// scrollPane moves the preview by half its height, down (dir 1) or up
// (-1): ctrl+j and ctrl+k, j and k as in Vim, which nothing types. Text
// scrolls through the lines a preview keeps (paneMaxLines), a folder
// through its entries; a preview is a look, reading is the editor's.
func (m *Model) scrollPane(dir int) {
	e, ok := m.paneCache[m.paneWant]
	if !m.paneShown() || !ok {
		return
	}
	n := len(e.code)
	if e.p.Kind == preview.Dir {
		n = len(e.p.Entries)
	}
	h := m.listHeight()
	m.paneScroll = min(max(m.paneScroll+dir*max(h/2, 1), 0), max(n-h, 0))
}
