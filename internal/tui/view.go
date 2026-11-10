package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Drawing the screen. View only reads the model; every cell is laid out
// here or in row.go and pane.go.

// View renders the whole screen: exactly height lines of exactly width cells.
func (m *Model) View() tea.View {
	if m.width <= 0 || m.height <= 0 {
		return tea.NewView("") // before the first WindowSizeMsg
	}
	if m.oil != nil {
		return m.viewOil()
	}

	var b strings.Builder
	b.WriteString(finish(m.viewPrompt(), m.width))
	b.WriteByte('\n')
	b.WriteString(m.viewHairline())
	b.WriteByte('\n')
	m.viewList(&b)
	b.WriteString(m.viewStatus())

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = "fndr"
	line, color := m.queryLine(), colLocal
	if m.rename != nil {
		line, color = m.rename.line, colWarn
	}
	v.Cursor = tea.NewCursor(min(promptWidth+ansi.StringWidth(safeText(line.text[:line.pos])), m.width-1), 0)
	v.Cursor.Shape = tea.CursorBar
	v.Cursor.Color = color
	return v
}

// viewPrompt renders the query line.
func (m *Model) viewPrompt() string {
	if m.rename != nil {
		return " " + stPromptRename.Render("❯") + " " + stQuery.Render(safeText(m.rename.line.text))
	}
	return " " + stPrompt.Render("❯") + " " + stQuery.Render(m.query)
}

// viewList writes listHeight lines: the visible results, or an empty state
// that says what fndr is doing, so the screen is never silently blank.
func (m *Model) viewList(b *strings.Builder) {
	h, w := m.listHeight(), m.listWidth()
	pane := m.paneLines(h) // nil without the pane

	var hint string
	switch {
	case m.query == "" && m.listErr != nil:
		hint = "cannot read this folder: " + safeText(m.listErr.Error())
	case m.query == "" && m.listing != nil && len(m.listing) == 0:
		hint = "empty folder"
	case m.query != "" && len(m.results) == 0 && !m.searching:
		hint = "no matches"
	}
	if hint != "" {
		for i := range h {
			line := ""
			if i == h/3 {
				line = stEmptyState.Render(centered(hint, w))
			}
			b.WriteString(finish(line, w))
			writePaneLine(b, pane, i)
			b.WriteByte('\n')
		}
		return
	}

	for i := range h {
		if r := m.offset + i; r < len(m.results) {
			habit := m.query == "" && r < m.habits
			b.WriteString(renderRow(&m.results[r], m.cwd, w, r == m.cursor, habit))
		} else {
			b.WriteString(strings.Repeat(" ", w))
		}
		writePaneLine(b, pane, i)
		b.WriteByte('\n')
	}
}

// viewStatus renders the status bar: scope and directory on the left, honest
// counts and problems on the right.
//
// When space runs out things give way in a fixed order. The scope block
// always stays. The notice (what just happened) is cut rather than dropped,
// and the counts make room for it. The directory, which is also visible in
// every result row, shrinks from the left: "…/fndr/cmd".
func (m *Model) viewStatus() string {
	mode := stMode.Render(" LOCAL ")
	if m.rename != nil {
		mode = stModeInsert.Render(" RENAME ") // keys are text now, as in OIL's INSERT
	}
	sep := stBar.Render("  ")
	// Cells left once the scope block, one cell of gap and one of right
	// padding are placed.
	avail := m.width - ansi.StringWidth(mode) - 2

	var right []string
	if m.crawlErr != nil {
		right = append(right, stBarErr.Render(safeText(m.crawlErr.Error())))
	}
	if m.skipped > 0 {
		right = append(right, stBarWarn.Render(fmt.Sprintf("%d unreadable", m.skipped)))
	}
	count := humanCount(m.ix.Len())
	if m.query != "" {
		count = humanCount(m.total) + "/" + count
	}
	if m.crawling {
		count = "indexing " + count
	}
	if m.rename != nil {
		count = "enter rename · esc cancel" // what counts now is how to finish
	}
	right = append(right, stBar.Render(count))
	rest := strings.Join(right, sep)

	if m.notice != "" {
		room := avail - ansi.StringWidth(rest) - ansi.StringWidth(sep)
		if room < 16 {
			rest, room = "", avail
		}
		if room > 0 {
			n := pick(m.noticeErr, &stBarErr, &stBarText).Render(ansi.Truncate(m.notice, room, "…"))
			if rest != "" {
				n += sep
			}
			rest = n + rest
		}
	}

	left := mode
	dir := safeText(m.tildify(m.cwd))
	if room := avail - ansi.StringWidth(rest) - 2; room >= 4 { // 2: spaces around dir
		left += stBarText.Render(" " + shortenLeft(dir, room) + " ")
	}

	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(rest) - 1
	if gap < 1 {
		return finish(left, m.width) // too narrow even for the counts
	}
	return left + stBar.Render(strings.Repeat(" ", gap)) + rest + stBar.Render(" ")
}

// shortenLeft cuts a path to room cells from the left, keeping the end,
// which says the most: "…/fndr/cmd".
func shortenLeft(path string, room int) string {
	if w := ansi.StringWidth(path); w > room {
		return "…" + ansi.TruncateLeft(path, w-room+1, "")
	}
	return path
}

// tildify shortens paths under $HOME to "~/...".
func (m *Model) tildify(p string) string {
	if m.home != "" && (p == m.home || strings.HasPrefix(p, m.home+string(filepath.Separator))) {
		return "~" + p[len(m.home):]
	}
	return p
}

// humanCount formats counts the way they are read at a glance: 999, 12.4k,
// 1.2M.
func humanCount(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 1_000_000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1e3)) + "k"
	default:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1e6)) + "M"
	}
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }

func centered(s string, width int) string {
	pad := max((width-ansi.StringWidth(s))/2, 0)
	return strings.Repeat(" ", pad) + s
}
