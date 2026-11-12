package tui

import (
	"fmt"
	"path/filepath"
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
// changes on the disk. ctrl+o enters it; esc, q or ctrl+o again go back to
// search, and :q quits fndr, as it quits Vim from its only window.
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

	// search is set for a buffer of search results (oilsearch.go): its
	// lines are paths from dir, and it is read again by path, not as a
	// folder.
	search *oilSearch

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
	// counted is the buffer's tick it was counted at: a key that only moves
	// the cursor leaves the tick, and Diff over the whole folder is skipped.
	changes int
	counted uint64

	// plan, once :w made one, is shown for y (oilwrite.go); nil while
	// editing. quitAfter: it came from :wq. applying: y was pressed and
	// the disk is being changed. wrote: something was written, so the
	// list must be read again on leaving.
	plan      mutation.Plan
	quitAfter bool

	// trashCounts says how many entries each folder to trash holds, by
	// its name in the plan; filled in off the UI goroutine (oilwrite.go).
	trashCounts map[string]int
	applying    bool
	wrote       bool
}

// oilMsg carries a folder read for OIL, and the name to put the cursor
// on: what was just created or renamed.
type oilMsg struct {
	dir     string
	entries []indexer.Entry
	err     error
	hidden  bool
	reveal  string

	search *oilSearch // results, not a folder (oilsearch.go)
	row    int        // for results read again: where the cursor was
}

// loadOil reads dir for OIL, off the UI goroutine.
func loadOil(dir string, hidden bool, reveal string) tea.Cmd {
	return func() tea.Msg {
		es, err := indexer.List(dir)
		return oilMsg{dir: dir, entries: es, err: err, hidden: hidden, reveal: reveal}
	}
}

// oilLoaded fills a buffer with the folder just read and enters OIL, or
// refills it after a write or a move to another folder. A refill keeps
// what the old state knew beyond the folder: whether anything was written.
//
// Moving to another folder in OIL moves fndr there too, so leaving OIL
// shows the list of the folder the user was editing. That happens only
// here, once the folder is read: a folder that cannot be read leaves both
// where they were, never one in each place.
func (m *Model) oilLoaded(msg *oilMsg) tea.Cmd {
	if msg.err != nil {
		what := "cannot edit this folder: "
		if msg.search != nil {
			what = "cannot edit what was found: "
		}
		m.setNotice(what+msg.err.Error(), true)
		return nil
	}
	st := &oilState{dir: msg.dir, hidden: msg.hidden, search: msg.search}
	if m.oil != nil {
		st.wrote = m.oil.wrote
	}
	var lines []mutation.Line
	for _, e := range msg.entries {
		// Hidden names are a folder's to hide; a search shows what it found.
		if msg.search == nil && (e.Name() == ".git" || !msg.hidden && strings.HasPrefix(e.Name(), ".")) {
			continue
		}
		name := oilLine(&e, msg.dir, msg.search != nil)
		id := len(st.items) + 1
		st.items = append(st.items, mutation.Item{ID: id, Name: name})
		st.entries = append(st.entries, e)
		lines = append(lines, mutation.Line{ID: id, Text: name})
	}
	st.buf = oil.New(lines)
	if msg.search != nil {
		st.buf.GoTo(min(msg.row, max(len(lines)-1, 0)))
		m.oil = st
		m.oilScroll()
		return nil // results stay where they were searched: no folder to move to
	}
	for i, l := range lines {
		if msg.reveal != "" && strings.TrimSuffix(l.Text, "/") == msg.reveal {
			st.buf.GoTo(i)
			break
		}
	}
	m.oil = st
	m.oilScroll()
	if msg.dir != m.cwd {
		return m.chdir(msg.dir, "")
	}
	return nil
}

// oilKey is handleKey in OIL: every key goes to the buffer, and what it
// asks for is done here.
func (m *Model) oilKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	st := m.oil
	if st.plan != nil {
		cmd := m.confirmKey(msg)
		return m, cmd
	}
	act, err := st.buf.Key(msg.String())
	if err != nil {
		m.setNotice(err.Error(), true)
	}
	st.recount()

	var cmd tea.Cmd
	switch act {
	case oil.Write, oil.WriteQuit:
		cmd = m.oilWrite(act == oil.WriteQuit)
	case oil.ForceQuit:
		_, cmd = m.quit()
	case oil.CopyPath:
		m.setNotice("gp copies paths once fndr has a clipboard", true)
	case oil.Help:
		m.setNotice("hjkl move · i a o type · dd trash · cc rename · u undo · :w write · esc back · :q quit · - up · g. hidden", false)
	case oil.Close, oil.Quit, oil.Open, oil.Parent, oil.Cwd, oil.Refresh, oil.ToggleHidden:
		// These leave the buffer as it is on the disk: with an edit not
		// written they would drop it, so they wait for :w, or u.
		if st.changes > 0 {
			m.setNotice("unsaved changes: :w writes them, u undoes, :q! quits without them", true)
			break
		}
		cmd = m.oilGo(act)
	}
	if m.oil != nil {
		m.oilScroll()
	}
	return m, cmd
}

// oilPaste puts pasted text into the buffer (oil.Buffer.Paste says
// where). Line ends of any kind become "\n", which may start new lines;
// tabs become spaces; other control characters are dropped, as everywhere
// text reaches the screen. While a plan waits for y, nothing is pasted.
func (m *Model) oilPaste(text string) {
	st := m.oil
	if st.plan != nil {
		return
	}
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", " ").Replace(text)
	st.buf.Paste(dropControls(text))
	st.recount()
	m.oilScroll()
}

// oilGo carries out an action that leaves the buffer: to another folder,
// the same one read again, a file opened, or out of OIL. The buffer has
// no edit to lose.
func (m *Model) oilGo(act oil.Action) tea.Cmd {
	st := m.oil
	line := st.buf.Line(st.buf.Row())
	here := strings.TrimSuffix(line.Text, "/") // to keep the cursor on it when the folder is read again
	switch act {
	case oil.Close:
		return m.leaveOil("")
	case oil.Quit:
		_, cmd := m.quit()
		return cmd
	case oil.Refresh:
		if st.search != nil {
			return st.oilReread()
		}
		return loadOil(st.dir, st.hidden, here)
	case oil.ToggleHidden:
		if st.search != nil {
			m.setNotice("g. is for a folder: results show what the search found", false)
			return nil
		}
		return loadOil(st.dir, !st.hidden, here)
	case oil.Cwd:
		return loadOil(m.start, st.hidden, "")
	case oil.Parent:
		if st.search != nil {
			return loadOil(st.dir, false, "") // up from results: the folder searched
		}
		parent := filepath.Dir(st.dir)
		if parent == st.dir {
			m.setNotice("already at the top", false)
			return nil
		}
		return loadOil(parent, st.hidden, filepath.Base(st.dir)) // the folder just left, selected
	case oil.Open:
		if line.ID == 0 {
			return nil // a blank line: nothing to open
		}
		e := &st.entries[line.ID-1]
		save := m.learn(e.Path)
		if !e.IsDir {
			return tea.Batch(save, m.open(e.Path))
		}
		dir, err := realDir(e.Path)
		if err != nil {
			m.setNotice(err.Error(), true)
			return save
		}
		return tea.Batch(save, loadOil(dir, st.hidden, ""))
	}
	return nil
}

// recount measures the edit. A buffer that cannot become a plan (two
// lines with one name) still counts as changed: leaving it must not
// silently drop what was typed.
func (st *oilState) recount() {
	if st.buf.Tick() == st.counted {
		return // a fresh buffer is at tick 0 with no changes, as counted starts
	}
	st.counted = st.buf.Tick()
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
	n := st.buf.Len()
	so := min(oilScrolloff, (h-1)/2)
	row := st.buf.Row()
	st.offset = min(st.offset, row-so)
	st.offset = max(st.offset, row+so-h+1)
	st.offset = min(max(st.offset, 0), max(n-h, 0))
}

// viewOil renders OIL: the folder's path, the buffer, and the status bar.
func (m *Model) viewOil() tea.View {
	st := m.oil
	h, w := m.listHeight(), m.width
	numW := max(len(strconv.Itoa(st.buf.Len())), 3)

	var b strings.Builder
	// For results, what found the lines and how many: an edit of results
	// says what it is an edit of. The folder gives way first, cut from
	// the left, as in the status bar.
	label := ""
	if st.search != nil {
		label = "   " + safeText(st.search.label) + "   " + fmt.Sprintf("%d found", len(st.items))
	}
	room := max(w-2-ansi.StringWidth(label), 1)
	b.WriteString(finish(" "+stQuery.Render(shortenLeft(safeText(m.tildify(st.dir)), room))+stDir.Render(label), w))
	b.WriteByte('\n')
	b.WriteString(stHair.Render(strings.Repeat("─", w)))
	b.WriteByte('\n')
	rows := make([]string, h)
	for i := range rows {
		if r := st.offset + i; r < st.buf.Len() {
			rows[i] = m.oilRow(st.buf.Line(r), r, numW)
		} else {
			rows[i] = strings.Repeat(" ", w)
		}
	}
	if st.plan != nil {
		overlay(rows, st.planBox(min(planMaxW, w-2), min(planMaxH, h)), w)
	}
	for _, r := range rows {
		b.WriteString(r)
		b.WriteByte('\n')
	}
	b.WriteString(m.oilStatus())

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = "fndr"
	if st.plan == nil { // the plan is read, not edited: no cursor
		v.Cursor = m.oilCursor(numW)
	}
	return v
}

// The plan pops up over the buffer in a box, as Oil.nvim's confirmation
// does, sized like the owner's Oil float (rounded, at most 80 by 30): the
// edit stays in sight around it, and the box says what it comes to.
const (
	planMaxW = 80
	planMaxH = 30
)

// planBox draws the plan as a box of lines of equal width, at most maxW
// by maxH cells: the question in the top border, a line per step (trashes
// in red, so they cannot be missed), and the keys. A plan taller than the
// box shows its start and how many steps more. Folders keep their "/",
// which mutation.Diff drops: trashing a folder must look like it.
func (st *oilState) planBox(maxW, maxH int) []string {
	dirs := make(map[string]bool)
	for i, it := range st.items {
		if st.entries[i].IsDir {
			dirs[strings.TrimSuffix(it.Name, "/")] = true
		}
	}
	name := func(n string) string {
		if dirs[n] {
			n += "/"
		}
		return safeText(n)
	}

	room := max(maxH-5, 1) // borders, a blank line above and below the steps, the keys
	var steps []string
	for i, op := range st.plan {
		if i == room-1 && len(st.plan) > room {
			steps = append(steps, stPlanVerb.Render(fmt.Sprintf("… and %d more", len(st.plan)-i)))
			break
		}
		switch op.Kind {
		case mutation.OpRename:
			to := safeText(op.To)
			if dirs[op.From] {
				to += "/"
			}
			steps = append(steps, stPlanVerb.Render("rename  ")+stName.Render(name(op.From)+" → "+to))
		case mutation.OpCreate:
			steps = append(steps, stPlanVerb.Render("create  ")+stName.Render(safeText(op.To)))
		case mutation.OpDelete:
			step := "trash   " + name(op.From)
			if n, ok := st.trashCounts[op.From]; ok {
				step += " " + itemCount(n)
			}
			steps = append(steps, stPlanTrash.Render(step))
		}
	}

	title := fmt.Sprintf("Apply %d changes?", len(st.plan))
	if len(st.plan) == 1 {
		title = "Apply 1 change?"
	}
	keys := stPlanTitle.Render("[y]") + stPlanVerb.Render("es   ") + stPlanTitle.Render("[n]") + stPlanVerb.Render("o")

	// As wide as the longest line needs, within maxW.
	inner := ansi.StringWidth(title) + 4
	for _, l := range steps {
		inner = max(inner, ansi.StringWidth(l)+4)
	}
	inner = max(min(max(inner, 24), maxW-2), 0) // a terminal narrower than any box still gets one, cut

	line := func(content string) string { // "│  content  │"
		return stPlanBorder.Render("│") + finish("  "+ansi.Truncate(content, max(inner-4, 0), "…"), inner) + stPlanBorder.Render("│")
	}
	title = ansi.Truncate(title, max(inner-3, 0), "…")
	top := stPlanBorder.Render("╭─ ") + stPlanTitle.Render(title) + stPlanBorder.Render(" "+strings.Repeat("─", max(inner-3-ansi.StringWidth(title), 0))+"╮")

	box := make([]string, 0, len(steps)+5)
	box = append(box, finish(top, inner+2), line(""))
	for _, l := range steps {
		box = append(box, line(l))
	}
	box = append(box, line(""), line(keys), stPlanBorder.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return box
}

// overlay draws box over rows, centred: each covered row keeps what is
// left and right of the box. rows are exactly width cells each.
func overlay(rows, box []string, width int) {
	if len(box) == 0 {
		return
	}
	bw := ansi.StringWidth(box[0])
	x, y := max((width-bw)/2, 0), max((len(rows)-len(box))/2, 0)
	for i, l := range box {
		if y+i >= len(rows) {
			break
		}
		row := rows[y+i]
		left := ansi.Truncate(row, x, "")
		left += strings.Repeat(" ", x-ansi.StringWidth(left)) // a wide character cut at x leaves a gap
		rows[y+i] = finish(left+l+ansi.TruncateLeft(row, x+bw, ""), width)
	}
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
	mode, name := pick(st.buf.Mode() == oil.Insert, &stModeInsert, &stMode), st.buf.Mode().String()
	if st.plan != nil {
		mode, name = &stModeConfirm, "CONFIRM"
	}
	left := mode.Render(" " + name + " ")
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
func (m *Model) oilCursor(numW int) *tea.Cursor {
	st := m.oil
	if st.buf.Mode() == oil.Command {
		c := tea.NewCursor(min(1+ansi.StringWidth(safeText(st.buf.CmdLine())), m.width-1), m.height-1)
		c.Shape = tea.CursorBar
		return c
	}
	text := safeText(st.buf.Line(st.buf.Row()).Text)
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
