package tui

import (
	"context"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/frecency"
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
	start  string         // directory fndr started in, where _ goes back to in OIL
	cwd    string         // directory shown and searched (LOCAL); always at or below local.root
	home   string         // $HOME, shown as "~"
	prune  *indexer.Prune // what the crawl lists without going in
	editor string         // editor command line; empty means $VISUAL, $EDITOR, then a fallback

	// local is what LOCAL search runs on: the index of the folder fndr
	// started in, or of wherever the user went that it does not cover.
	local *scope

	// global is the whole home, for GLOBAL search (global.go): nil until
	// Tab first asks for it. globalOn: search runs on it now.
	global   *scope
	globalOn bool

	// hist is what fndr has learned about where the user goes: entered
	// folders, opened files, jumps. nil when it could not be opened; fndr
	// then works as before and just does not learn. boosts is its snapshot
	// for search: replaced after every visit, never modified, so parallel
	// searches read it without locks.
	hist   *frecency.Store
	boosts map[string]int

	// ctx lives as long as the program; quitting cancels it, which stops the
	// crawl and any running search.
	ctx  context.Context
	stop context.CancelFunc

	// crawlGen numbers the crawls of every scope (see scope.gen).
	crawlGen int

	// With an empty query the list shows cwd's contents (browse); otherwise
	// search results. listing is nil until the directory has been read.
	listing []indexer.Result
	habits  int // the first habits rows of listing are places the user goes, not contents
	listErr error
	reveal  string // path to select when the listing arrives: the folder just left

	query   string
	qpos    int              // text cursor: byte offset into query, on a rune boundary
	results []indexer.Result // what the list shows: the listing or search results
	total   int              // matches in the index, of which results holds the best

	// oil is the folder being edited in OIL mode (oil.go), nil outside it.
	oil *oilState

	// rename is the path ctrl+r is editing (rename.go); nil otherwise.
	rename *renameState

	// Searches run off the UI goroutine. Every new search gets a fresh seq and
	// cancels the previous one; a reply whose seq is not the latest is stale
	// (the user has typed since) and is dropped.
	seq          int
	searching    bool
	cancelSearch context.CancelFunc

	cursor int // selected row in results
	offset int // first visible row

	// picked is the path the user moved the selection to, "" until they
	// do. While the crawl runs, the same query is searched again and again
	// and better matches push rows down; the selection follows picked, not
	// the row number, or enter would open whatever slid under the cursor.
	// A new query or folder starts over at the top.
	picked string
	width  int
	height int

	// preview is the ctrl+v switch. Off by default: most of the time the list
	// is all that is needed, and with the pane off nothing is read at all.
	preview bool

	// paneWant is the path the pane should show: the selection, once the
	// pane is shown. paneCache holds previews already read, by path; see
	// pane.go for how they are loaded.
	paneWant  string
	paneCache map[string]paneEntry

	// Pictures in the pane (image.go). graphics: the terminal can show
	// them; tmux: escape sequences must be wrapped to pass through it.
	// imageOwner maps each image id handed out to the path it shows.
	graphics, tmux bool
	imageSeq       int
	imageOwner     [256]string

	// exitDir is the folder the shell should change to once fndr exits: set
	// by ctrl+e, left empty by esc and ctrl+c, which leave the shell where
	// it was.
	exitDir string

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
	m.local.crawling = true // Init starts it
	m.graphics, m.tmux = detectGraphics(os.Getenv)

	hist, err := openHistory()
	if err != nil {
		m.setNotice("not learning this time: "+err.Error(), true)
	}
	m.useHistory(hist)
	return m
}

// newModel builds a model around an existing index without starting a crawl,
// so tests can control exactly what is indexed.
func newModel(root, home string, ix *indexer.Index) *Model {
	ctx, stop := context.WithCancel(context.Background())
	return &Model{
		cwd: root, start: root, home: home, prune: indexer.DefaultPrune(home),
		local: &scope{root: root, ix: ix},
		ctx:   ctx, stop: stop,
	}
}

// Init reads the starting directory and starts the crawl.
func (m *Model) Init() tea.Cmd {
	var passthrough tea.Cmd
	if m.graphics && m.tmux {
		passthrough = allowPassthrough
	}
	if !m.local.crawling {
		return tea.Batch(m.load(m.cwd), passthrough)
	}
	return tea.Batch(m.load(m.cwd), m.startCrawl(m.local), passthrough)
}

// Update applies one event to the model. After every event the preview pane
// catches up with the selection, wherever the event moved it: a key, a search
// reply, a folder listing.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	return m, tea.Batch(cmd, m.wantPreview())
}

// update does the work of Update for one event; Update wraps it so the pane
// check cannot be forgotten on any of its many return paths.
func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollToCursor()
		if m.oil != nil {
			m.oilScroll()
		}
		return m, nil

	case oilMsg:
		cmd := m.oilLoaded(&msg)
		return m, cmd

	case oilWrittenMsg:
		cmd := m.oilWritten(&msg)
		return m, cmd
	case renamedMsg:
		cmd := m.renamed(&msg)
		return m, cmd

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		if m.oil != nil {
			m.oilPaste(msg.Content)
			return m, nil
		}
		// A pasted path or name becomes the query. Whitespace runs, newlines
		// included, become one space; other control characters, such as
		// the ESC of an escape sequence, are dropped: the line is printed.
		text := strings.Join(strings.Fields(dropControls(msg.Content)), " ")
		if m.rename != nil {
			m.rename.line.insert(text)
			return m, nil
		}
		l := m.queryLine()
		l.insert(text)
		cmd := m.setQueryLine(l)
		return m, cmd

	case crawlStartedMsg:
		cmd := m.crawlStarted(&msg)
		return m, cmd

	case batchMsg:
		cmd := m.batch(&msg)
		return m, cmd

	case crawlDoneMsg:
		cmd := m.crawlDone(&msg)
		return m, cmd

	case searchMsg:
		cmd := m.searched(&msg)
		return m, cmd

	case listMsg:
		m.listed(&msg)
		return m, nil

	case openMsg:
		return m, msg.run()

	case historySavedMsg:
		cmd := m.historySaved(msg)
		return m, cmd

	case openedMsg:
		m.opened(msg)
		// The file may have just been edited: forget previews read before.
		m.paneCache = nil
		return m, nil

	case paneTickMsg:
		cmd := m.paneTick(msg)
		return m, cmd

	case paneMsg:
		m.paneLoaded(&msg)
		return m, nil

	case paneImageMsg:
		cmd := m.imageLoaded(&msg)
		return m, cmd
	}
	return m, nil
}

// ExitDir returns the folder the shell should change to after the program
// exits, or "" to stay where it was.
func (m *Model) ExitDir() string { return m.exitDir }

// quit stops every crawl and search and ends the program.
func (m *Model) quit() (tea.Model, tea.Cmd) {
	m.stop()
	if m.imageSeq > 0 { // pictures were sent: free them before leaving
		return m, tea.Sequence(tea.Raw(deleteImages(m.tmux)), tea.Quit)
	}
	return m, tea.Quit
}
