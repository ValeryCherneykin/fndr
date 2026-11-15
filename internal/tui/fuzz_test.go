package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// FuzzKeys plays fndr as a person would, only faster and with no patience:
// keys, and the background work they start (reading the saved index,
// crawls, searches, writes) answering in any order, a key often coming
// before the reply to the one before it. That order is where the races
// are: Tab pressed while the saved index was being read left search dead,
// and no test pressing keys one at a time, each fully answered, found it.
//
// After every step the screen must be exactly its size and the selection
// in the list; once every command has run, no index may still be
// "indexing", with nothing left that could ever finish it.
//
// Everything happens in a throwaway home: the trash, the index file. A
// command that would start a program (an editor, the Finder) is dropped,
// never run.
func FuzzKeys(f *testing.F) {
	tab, run := fuzzKeyIndex("tab"), byte(200)
	f.Add([]byte{0, tab, run, run, run})                     // in the home, Tab before the saved index
	f.Add([]byte{2, tab, run, run, run})                     // below it, the same
	f.Add([]byte{1, 'm' % 64, run, tab, run, tab, run, run}) // no saved index
	f.Add([]byte{3, fuzzKeyIndex("ctrl+o"), run, run, fuzzKeyIndex("d"), fuzzKeyIndex("d"), run})

	f.Fuzz(func(t *testing.T, script []byte) {
		if len(script) == 0 || len(script) > 300 {
			return
		}
		home := fuzzHome(t)
		mode := script[0]
		cwd := home
		if mode&2 != 0 {
			cwd = filepath.Join(home, "src")
		}
		m := newModel(cwd, home, &indexer.Index{})
		m.local.crawling = true // Init starts it, as New says
		m.release = func() {}
		if mode&1 == 0 { // a saved index from a run before
			m.savedPath = filepath.Join(home, ".cache", "fndr", "index")
			saveFuzzIndex(t, m, home)
		}
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})

		d := &fuzzDriver{t: t, m: m, home: home}
		d.push(m.Init())
		for _, b := range script[1:] {
			if b >= run {
				d.runOne(int(b - run))
			} else {
				d.press(fuzzKeys[int(b)%len(fuzzKeys)])
			}
			if d.quit {
				return
			}
			d.check()
		}
		for steps := 0; len(d.queue) > 0; steps++ {
			if steps > 5000 {
				t.Fatalf("commands never run out: %d left", len(d.queue))
			}
			d.runOne(0)
			if d.quit {
				return
			}
		}
		d.check()
		for _, s := range append(m.scopes(), m.global) {
			if s != nil && s.crawling {
				t.Fatalf("%s is indexing with nothing left to finish it (started %v)", s.root, s.stop != nil)
			}
		}
		if m.globalOn && m.global == nil {
			t.Fatal("GLOBAL on, with no index")
		}
	})
}

// fuzzKeys are the keys FuzzKeys presses: fndr's own, what OIL takes, and
// letters that find something in fuzzHome.
var fuzzKeys = []string{
	"tab", "enter", "esc", "backspace", "up", "down", "pgdown", "home",
	"ctrl+n", "ctrl+p", "ctrl+o", "ctrl+r", "ctrl+t", "ctrl+u", "ctrl+v",
	"ctrl+j", "ctrl+k", "ctrl+y", "ctrl+g", "ctrl+w", "ctrl+h", "ctrl+l", "f1",
	"m", "a", "i", "n", "o", ".", "-", "/", "d", "y", "p", "j", "k", "x",
	":", "w", "q", "u", "G", "g", "c", "has:", "@", " ",
}

// fuzzKeyIndex is the script byte that presses k.
func fuzzKeyIndex(k string) byte {
	for i, f := range fuzzKeys {
		if f == k {
			return byte(i) //nolint:gosec // fewer keys than 200, the bytes that run commands
		}
	}
	panic(k)
}

// fuzzKey is the key press named k.
func fuzzKey(k string) tea.KeyPressMsg {
	codes := map[string]rune{
		"tab": tea.KeyTab, "enter": tea.KeyEnter, "esc": tea.KeyEscape, "backspace": tea.KeyBackspace,
		"up": tea.KeyUp, "down": tea.KeyDown, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "f1": tea.KeyF1,
	}
	if c, ok := codes[k]; ok {
		return tea.KeyPressMsg{Code: c}
	}
	if c, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(c[0]), Mod: tea.ModCtrl}
	}
	r, _ := utf8.DecodeRuneInString(k)
	return tea.KeyPressMsg{Code: r, Text: k}
}

// fuzzHome is a small home: a project, a hidden folder, a few names to find.
func fuzzHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home) // the trash
	t.Setenv("XDG_DATA_HOME", "")
	for _, f := range []string{"README.md", "src/main.go", "src/util.go", "docs/notes.md", ".config/x", "a b/ñ.txt"} {
		p := filepath.Join(home, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func saveFuzzIndex(t *testing.T, m *Model, home string) {
	t.Helper()
	j := func(p string) string { return filepath.Join(home, p) }
	err := indexer.Save(m.savedPath, indexer.Saved{Root: home, Rules: m.prune.Rules(), Entries: []indexer.Entry{
		indexer.NewEntry(j("README.md"), 2, 1, false),
		indexer.NewEntry(j("gone.txt"), 2, 1, false), // not there now: the catch-up finds out
		indexer.NewEntry(j("src"), 1, 1, true),
		indexer.NewEntry(j("src/main.go"), 2, 1, false),
	}})
	if err != nil {
		t.Fatal(err)
	}
}

// fuzzDriver is the Bubble Tea runtime, played by hand: the commands it
// is given wait in a queue until the script says which runs next.
type fuzzDriver struct {
	t     *testing.T
	m     *Model
	home  string // where the script stays (press)
	queue []tea.Cmd
	quit  bool
}

func (d *fuzzDriver) push(cmd tea.Cmd) {
	if cmd != nil {
		d.queue = append(d.queue, cmd)
	}
}

func (d *fuzzDriver) press(k string) {
	// Up from the home would leave it for the disk around it: a crawl of
	// the machine fuzzed on, slow, and not what is tested. The fence is
	// here, not in fndr: going up is fndr's to allow.
	m := d.m
	if up := k == "backspace" || k == "-"; up && m.home == d.home {
		if m.oil != nil && m.oil.dir == d.home && m.oil.plan == nil {
			return
		}
		if m.oil == nil && m.rename == nil && m.cwd == d.home && m.query == "" && len(m.tags) == 0 {
			return
		}
	}
	_, cmd := d.m.Update(fuzzKey(k))
	d.push(cmd)
}

// runOne runs the i-th waiting command (counted round the queue) and
// gives its message to the model.
func (d *fuzzDriver) runOne(i int) {
	if len(d.queue) == 0 {
		return
	}
	i %= len(d.queue)
	cmd := d.queue[i]
	d.queue = append(d.queue[:i], d.queue[i+1:]...)

	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-got:
	case <-time.After(10 * time.Second):
		d.t.Fatal("a command never returned")
	}
	switch msg := msg.(type) {
	case nil:
	case tea.QuitMsg:
		d.quit = true
	case tea.BatchMsg:
		for _, c := range msg {
			d.push(c)
		}
	case openMsg:
		// Would start an editor, an app or the Finder: dropped. What
		// fndr does with the reply is tested where it is run on purpose.
	default:
		_, cmd := d.m.Update(msg)
		d.push(cmd)
	}
}

// check holds what must hold after every step.
func (d *fuzzDriver) check() {
	d.t.Helper()
	m := d.m
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) != m.height {
		d.t.Fatalf("%d lines on a screen of %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.width {
			d.t.Fatalf("line %d is %d cells, the screen %d: %q", i, w, m.width, ansi.Strip(l))
		}
	}
	if len(m.results) > 0 && (m.cursor < 0 || m.cursor >= len(m.results)) {
		d.t.Fatalf("the cursor on row %d of %d", m.cursor, len(m.results))
	}
}
