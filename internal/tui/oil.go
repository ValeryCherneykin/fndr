package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
	"github.com/ValeryCherneykin/fndr/internal/oil"
)

// OIL mode edits the current folder as text, the way Oil.nvim does: one
// name per line, Vim keys (package oil), and :w to turn the edit into
// changes on the disk. ctrl+o enters it; :q leaves.
//
// Everything OIL needs lives in oilState, and m.oil is nil outside the
// mode. So OIL's fields cannot be read by mistake while searching, and
// "am I in OIL" is one nil check, not a flag that can disagree with them.

// oilScrolloff is how many lines stay visible above and below the cursor,
// as scrolloff = 8 in the owner's Neovim.
const oilScrolloff = 8

// oilState is a folder being edited.
type oilState struct {
	dir string
	buf *oil.Buffer

	// items are the entries the buffer was filled with, by id: items[i]
	// has id i+1, and entries[i] is the same entry with its size and
	// time. Only what is shown is here: an entry left out of the buffer
	// (hidden, .git) must not be in items either, or mutation.Diff would
	// read its missing line as "delete it".
	items   []mutation.Item
	entries []indexer.Entry

	hidden bool // names starting with "." are shown
	offset int  // first line on screen

	// changes is how many steps the edit comes to, kept up to date after
	// every key: the status bar shows it, and :q refuses to drop them.
	changes int
}

// oilMsg carries a folder read for OIL.
type oilMsg struct {
	dir     string
	entries []indexer.Entry
	err     error
	hidden  bool
}

// loadOil reads dir for OIL, off the UI goroutine.
func loadOil(dir string, hidden bool) tea.Cmd {
	return func() tea.Msg {
		es, err := indexer.List(dir)
		return oilMsg{dir: dir, entries: es, err: err, hidden: hidden}
	}
}

// oilLoaded fills a buffer with the folder just read and enters OIL.
func (m *Model) oilLoaded(msg *oilMsg) {
	if msg.err != nil {
		m.setNotice("cannot edit this folder: "+msg.err.Error(), true)
		return
	}
	st := &oilState{dir: msg.dir, hidden: msg.hidden}
	var lines []mutation.Line
	for _, e := range msg.entries {
		name := e.Name()
		if name == ".git" || !msg.hidden && strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir {
			name += "/"
		}
		id := len(st.items) + 1
		st.items = append(st.items, mutation.Item{ID: id, Name: name})
		st.entries = append(st.entries, e)
		lines = append(lines, mutation.Line{ID: id, Text: name})
	}
	st.buf = oil.New(lines)
	m.oil = st
}

// oilKey is handleKey in OIL: every key goes to the buffer, and what it
// asks for is done here.
func (m *Model) oilKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	st := m.oil
	act, err := st.buf.Key(msg.String())
	if err != nil {
		m.setNotice(err.Error(), true)
	}
	st.recount()

	switch act {
	case oil.Quit:
		if st.changes > 0 {
			m.setNotice("unsaved changes: :w writes them, :q! drops them", true)
			break
		}
		m.oil = nil
	case oil.ForceQuit:
		m.oil = nil
	case oil.Write, oil.WriteQuit:
		m.setNotice("writing is not wired up yet: :q! to leave", true) // until the plan screen lands
	}
	if m.oil != nil {
		m.oilScroll()
	}
	return m, nil
}

// recount measures the edit. A buffer that cannot become a plan (two
// lines with one name) still counts as changed: leaving it must not
// silently drop what was typed.
func (st *oilState) recount() {
	plan, err := mutation.Diff(st.items, st.buf.Lines())
	st.changes = len(plan)
	if err != nil {
		st.changes = max(st.changes, 1)
	}
}

// oilScroll keeps the cursor on screen, with oilScrolloff lines around it
// where the buffer has them.
func (m *Model) oilScroll() {
	st, h := m.oil, m.listHeight()
	n := len(st.buf.Lines())
	so := min(oilScrolloff, (h-1)/2)
	row := st.buf.Row()
	st.offset = min(st.offset, row-so)
	st.offset = max(st.offset, row+so-h+1)
	st.offset = min(max(st.offset, 0), max(n-h, 0))
}

// viewOil renders OIL: the folder's path, the buffer, and the status bar.
func (m *Model) viewOil() tea.View {
	st := m.oil
	lines := st.buf.Lines()
	h, w := m.listHeight(), m.width
	numW := max(len(strconv.Itoa(len(lines))), 3)

	var b strings.Builder
	b.WriteString(finish(" "+stQuery.Render(shortenLeft(safeText(m.tildify(st.dir)), max(w-2, 1))), w))
	b.WriteByte('\n')
	b.WriteString(stHair.Render(strings.Repeat("─", w)))
	b.WriteByte('\n')
	for i := range h {
		if r := st.offset + i; r < len(lines) {
			b.WriteString(m.oilRow(lines[r], r, numW))
		} else {
			b.WriteString(strings.Repeat(" ", w))
		}
		b.WriteByte('\n')
	}
	b.WriteString(m.oilStatus())

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = "fndr"
	v.Cursor = m.oilCursor(lines, numW)
	return v
}

// oilRow renders line r of the buffer: its number, relative to the
// cursor's line as with relativenumber, an icon, the name, and, for an
// entry that was loaded, its size and date, dim, at the right edge.
func (m *Model) oilRow(l mutation.Line, r, numW int) string {
	st := m.oil
	num := strconv.Itoa(abs(r - st.buf.Row()))
	if r == st.buf.Row() {
		num = fmt.Sprintf("%-*d", numW, r+1) // the cursor's line: its own number, as in Vim
	} else {
		num = fmt.Sprintf("%*s", numW, num)
	}

	name := safeText(l.Text) // same length: the cursor's offset still fits
	isDir := strings.HasSuffix(name, "/")
	var facts string
	if l.ID > 0 {
		e := &st.entries[l.ID-1]
		isDir = e.IsDir
		facts = oilFacts(e)
	}

	nameSt := pick(strings.HasPrefix(name, "."), &stNameHidden, &stName)
	left := stDir.Render(num) + " " + iconFor(strings.TrimSuffix(name, "/"), isDir) + " " + nameSt.Render(name)
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(facts) - 1
	if facts == "" || gap < 2 {
		return finish(left, m.width)
	}
	return finish(left+strings.Repeat(" ", gap)+stDir.Render(facts), m.width)
}

// oilFacts is the size and date of an entry, as Oil's size and mtime
// columns show them.
func oilFacts(e *indexer.Entry) string {
	t := time.Unix(0, e.ModTime)
	date := t.Format("2 Jan")
	if t.Year() != time.Now().Year() {
		date = t.Format("Jan 2006")
	}
	size := ""
	if !e.IsDir {
		size = humanSize(e.Size)
	}
	return fmt.Sprintf("%7s  %8s", size, date)
}

// oilStatus is the status bar in OIL: the mode, the folder, and how many
// changes are waiting for :w. Typing a command, it is the command line.
func (m *Model) oilStatus() string {
	st := m.oil
	if st.buf.Mode() == oil.Command {
		return finish(stBarText.Render(":"+safeText(st.buf.CmdLine())), m.width)
	}
	// What gives way when space runs out: the folder first, cut from the
	// left; the notice or the count of changes, which say what is at
	// stake, last.
	mode := pick(st.buf.Mode() == oil.Insert, &stModeNew, &stMode)
	left := mode.Render(" " + st.buf.Mode().String() + " ")
	right := ""
	switch {
	case m.notice != "":
		right = pick(m.noticeErr, &stBarErr, &stBarText).Render(ansi.Truncate(m.notice, max(m.width-ansi.StringWidth(left)-2, 0), "…"))
	case st.changes == 1:
		right = stBarWarn.Render("1 change")
	case st.changes > 1:
		right = stBarWarn.Render(fmt.Sprintf("%d changes", st.changes))
	}
	if room := m.width - ansi.StringWidth(left) - ansi.StringWidth(right) - 4; room >= 4 { // spaces around the folder and before the right
		left += stBarText.Render(" " + shortenLeft(safeText(m.tildify(st.dir)), room) + " ")
	}
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right) - 1
	if gap < 1 {
		return finish(left, m.width)
	}
	return left + stBar.Render(strings.Repeat(" ", gap)) + right + stBar.Render(" ")
}

// oilCursor places the terminal's cursor on the buffer's: a block on a
// character in Normal mode, a bar between characters while typing, and
// at the end of the command line while one is typed.
func (m *Model) oilCursor(lines []mutation.Line, numW int) *tea.Cursor {
	st := m.oil
	if st.buf.Mode() == oil.Command {
		c := tea.NewCursor(min(1+ansi.StringWidth(safeText(st.buf.CmdLine())), m.width-1), m.height-1)
		c.Shape = tea.CursorBar
		return c
	}
	text := safeText(lines[st.buf.Row()].Text)
	x := numW + 1 + 2 + ansi.StringWidth(text[:st.buf.Col()]) // number, space, icon and its space
	c := tea.NewCursor(min(x, m.width-1), 2+st.buf.Row()-st.offset)
	c.Color = colLocal
	c.Shape = tea.CursorBlock
	if st.buf.Mode() == oil.Insert {
		c.Shape, c.Color = tea.CursorBar, colWarn
	}
	return c
}

func abs(n int) int { return max(n, -n) }
