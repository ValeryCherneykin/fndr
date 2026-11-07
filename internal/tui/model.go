package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// Rows taken by everything except the result list: query line, hairline,
// status bar.
const chromeHeight = 3

// promptWidth is the width of " ❯ " before the query, where the cursor starts.
const promptWidth = 3

// Model is the state of the whole interface. Bubble Tea calls Update with
// every event and View after every Update. Only Update changes the model; View
// only reads it, which is what makes the interface testable without a
// terminal: feed messages to Update, inspect the result.
type Model struct {
	root   string // directory being indexed and searched (LOCAL scope)
	home   string // $HOME, shown as "~"
	editor string // editor command line; empty means $VISUAL, $EDITOR, then a fallback

	ix *indexer.Index

	// ctx lives as long as the program; quitting cancels it, which stops the
	// crawl and any running search.
	ctx  context.Context
	stop context.CancelFunc

	crawl    *crawlStream
	crawling bool
	skipped  int
	crawlErr error

	query   string
	results []indexer.Result
	total   int // matches in the index, of which results holds the best

	// Searches run off the UI goroutine. Every new search gets a fresh seq and
	// cancels the previous one; a reply whose seq is not the latest is stale
	// (the user has typed since) and is dropped.
	seq          int
	searching    bool
	cancelSearch context.CancelFunc

	cursor int // selected row in results
	offset int // first visible row
	width  int
	height int

	// notice reports the last action in the status bar until the next key:
	// what was opened, or why it could not be.
	notice    string
	noticeErr bool
}

// New returns a model that will index and search root once the program
// starts.
func New(root string) *Model {
	home, _ := os.UserHomeDir() // best effort: without it paths just are not shortened
	m := newModel(root, home, &indexer.Index{})
	m.crawling = true
	return m
}

// newModel builds a model around an existing index without starting a crawl,
// so tests can control exactly what is indexed.
func newModel(root, home string, ix *indexer.Index) *Model {
	ctx, stop := context.WithCancel(context.Background())
	return &Model{root: root, home: home, ix: ix, ctx: ctx, stop: stop}
}

// Messages produced by commands. Each carries exactly what Update needs.
type (
	crawlStartedMsg struct{ stream *crawlStream }
	batchMsg        []indexer.Entry
	crawlDoneMsg    struct {
		stats indexer.Stats
		err   error
	}
	searchMsg struct {
		seq     int
		results []indexer.Result
		total   int
		indexed int // snapshot length the search ran on
		err     error
	}
)

// Init starts the crawl.
func (m *Model) Init() tea.Cmd {
	if !m.crawling {
		return nil
	}
	return startCrawl(m.ctx, m.root)
}

// Update applies one event to the model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollToCursor()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		// A pasted path or name becomes the query; newlines would break the
		// single-line prompt.
		text := strings.Join(strings.Fields(msg.Content), " ")
		cmd := m.setQuery(m.query + text)
		return m, cmd

	case crawlStartedMsg:
		m.crawl = msg.stream
		return m, m.crawl.next()

	case batchMsg:
		m.ix.Add(msg)
		cmd := m.crawl.next()
		// Refresh results as the index grows, but never queue searches: if one
		// is running, its reply triggers the next (see searchMsg).
		if m.query != "" && !m.searching {
			return m, tea.Batch(cmd, m.search())
		}
		return m, cmd

	case crawlDoneMsg:
		m.crawling = false
		m.skipped = msg.stats.Skipped
		if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
			m.crawlErr = msg.err
		}
		if m.query != "" && !m.searching {
			cmd := m.search()
			return m, cmd
		}
		return m, nil

	case searchMsg:
		if msg.seq != m.seq {
			return m, nil // stale: the query changed while it ran
		}
		m.searching = false
		if msg.err != nil {
			return m, nil // canceled; a newer search is already on its way
		}
		m.results, m.total = msg.results, msg.total
		m.cursor = min(m.cursor, max(len(m.results)-1, 0))
		m.scrollToCursor()
		// The crawl added entries while this search ran: search again, so the
		// list keeps up with indexing without a search per batch.
		if m.ix.Len() > msg.indexed {
			cmd := m.search()
			return m, cmd
		}
		return m, nil

	case openMsg:
		return m, msg.run()

	case openedMsg:
		m.opened(msg)
		return m, nil
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.setNotice("", false) // a notice describes the last action; any key moves on

	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		// Esc is always one step back: first the query, then the program.
		if m.query != "" {
			cmd := m.setQuery("")
			return m, cmd
		}
		return m.quit()

	case "enter":
		cmd := m.openSelected()
		return m, cmd

	case "up", "ctrl+p", "ctrl+k", "alt+k":
		m.move(-1)
	case "down", "ctrl+n", "ctrl+j", "alt+j":
		m.move(1)
	case "pgup":
		m.move(-m.listHeight())
	case "pgdown":
		m.move(m.listHeight())
	case "home":
		m.move(-len(m.results))
	case "end":
		m.move(len(m.results))

	case "backspace":
		if m.query != "" {
			_, size := utf8.DecodeLastRuneInString(m.query)
			cmd := m.setQuery(m.query[:len(m.query)-size])
			return m, cmd
		}
	case "ctrl+w":
		cmd := m.setQuery(deleteLastWord(m.query))
		return m, cmd
	case "ctrl+u":
		cmd := m.setQuery("")
		return m, cmd

	default:
		// Printable input. Ctrl and Alt chords are commands, never text.
		if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			cmd := m.setQuery(m.query + msg.Text)
			return m, cmd
		}
	}
	return m, nil
}

func (m *Model) quit() (tea.Model, tea.Cmd) {
	m.stop()
	return m, tea.Quit
}

// setQuery replaces the query, resets the selection to the top, and starts a
// search for it.
func (m *Model) setQuery(q string) tea.Cmd {
	if q == m.query {
		return nil
	}
	m.query = q
	m.cursor, m.offset = 0, 0
	return m.search()
}

// search cancels any running search and starts one for the current query on
// a snapshot of the index. The search itself runs in the returned command,
// off the UI goroutine.
func (m *Model) search() tea.Cmd {
	if m.cancelSearch != nil {
		m.cancelSearch()
	}
	m.seq++
	if m.query == "" {
		m.results, m.total, m.searching, m.cancelSearch = nil, 0, false, nil
		return nil
	}

	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelSearch = cancel
	m.searching = true

	seq, snap, q := m.seq, m.ix.Snapshot(), indexer.Query{Text: m.query}
	return func() tea.Msg {
		defer cancel() // release the context once done; canceling twice is harmless
		rs, total, err := indexer.Search(ctx, snap, q)
		return searchMsg{seq: seq, results: rs, total: total, indexed: len(snap), err: err}
	}
}

func (m *Model) move(delta int) {
	if len(m.results) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.results)-1)
	m.scrollToCursor()
}

// scrollToCursor adjusts the viewport so the selected row is visible, moving
// it as little as possible.
func (m *Model) scrollToCursor() {
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = min(max(m.offset, 0), max(len(m.results)-h, 0))
}

func (m *Model) listHeight() int {
	return max(m.height-chromeHeight, 1)
}

// View renders the whole screen: exactly height lines of exactly width cells.
func (m *Model) View() tea.View {
	if m.width <= 0 || m.height <= 0 {
		return tea.NewView("") // before the first WindowSizeMsg
	}

	var b strings.Builder
	b.WriteString(finish(" "+stPrompt.Render("❯")+" "+stQuery.Render(m.query), m.width))
	b.WriteByte('\n')
	b.WriteString(stHair.Render(strings.Repeat("─", m.width)))
	b.WriteByte('\n')
	m.viewList(&b)
	b.WriteString(m.viewStatus())

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = "fndr"
	v.Cursor = tea.NewCursor(min(promptWidth+ansi.StringWidth(m.query), m.width-1), 0)
	v.Cursor.Shape = tea.CursorBar
	v.Cursor.Color = colLocal
	return v
}

// viewList writes listHeight lines: the visible results, or an empty state
// that says what fndr is doing, so the screen is never silently blank.
func (m *Model) viewList(b *strings.Builder) {
	h := m.listHeight()

	var hint string
	switch {
	case m.query == "" && m.crawling:
		hint = "indexing " + humanCount(m.ix.Len()) + " files…"
	case m.query == "":
		hint = "type to search " + humanCount(m.ix.Len()) + " files"
	case len(m.results) == 0 && !m.searching:
		hint = "no matches"
	}
	if hint != "" {
		for i := range h {
			line := ""
			if i == h/3 {
				line = stEmptyState.Render(centered(hint, m.width))
			}
			b.WriteString(finish(line, m.width))
			b.WriteByte('\n')
		}
		return
	}

	for i := range h {
		if r := m.offset + i; r < len(m.results) {
			b.WriteString(renderRow(&m.results[r], m.root, m.width, r == m.cursor))
		} else {
			b.WriteString(strings.Repeat(" ", m.width))
		}
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
	sep := stBar.Render("  ")
	// Cells left once the scope block, one cell of gap and one of right
	// padding are placed.
	avail := m.width - ansi.StringWidth(mode) - 2

	var right []string
	if m.crawlErr != nil {
		right = append(right, stBarErr.Render(m.crawlErr.Error()))
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
	dir := m.tildify(m.root)
	if room := avail - ansi.StringWidth(rest) - 2; room >= 4 { // 2: spaces around dir
		if w := ansi.StringWidth(dir); w > room {
			dir = "…" + ansi.TruncateLeft(dir, w-room+1, "")
		}
		left += stBarText.Render(" " + dir + " ")
	}

	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(rest) - 1
	if gap < 1 {
		return finish(left, m.width) // too narrow even for the counts
	}
	return left + stBar.Render(strings.Repeat(" ", gap)) + rest + stBar.Render(" ")
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

// deleteLastWord implements ctrl+w as in a shell: drop trailing spaces, then
// the word before them.
func deleteLastWord(s string) string {
	s = strings.TrimRight(s, " ")
	if i := strings.LastIndexByte(s, ' '); i >= 0 {
		return s[:i+1]
	}
	return ""
}

// crawlStream connects the crawl goroutine to the UI. Batches are pulled one
// at a time by a command, the standard Bubble Tea way to consume a channel:
// each batchMsg schedules the read of the next one.
type crawlStream struct {
	batches chan []indexer.Entry
	done    chan crawlDoneMsg
}

// startCrawl launches indexer.Crawl in its own goroutine. A small buffer lets
// the crawl run a few batches ahead of the UI instead of waiting on every
// handoff. The goroutine always ends: Crawl returns when ctx is canceled, and
// done has room for its one result even if nobody reads it.
func startCrawl(ctx context.Context, root string) tea.Cmd {
	return func() tea.Msg {
		s := &crawlStream{
			batches: make(chan []indexer.Entry, 4),
			done:    make(chan crawlDoneMsg, 1),
		}
		go func() {
			stats, err := indexer.Crawl(ctx, root, s.batches)
			close(s.batches)
			s.done <- crawlDoneMsg{stats: stats, err: err}
		}()
		return crawlStartedMsg{stream: s}
	}
}

// next returns a command that waits for the next batch, or for the final
// result once the batches are exhausted.
func (s *crawlStream) next() tea.Cmd {
	return func() tea.Msg {
		if batch, ok := <-s.batches; ok {
			return batchMsg(batch)
		}
		return <-s.done
	}
}
