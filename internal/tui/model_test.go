package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// testModel returns a model over a fixed index, sized like a small terminal.
// No crawl runs: tests control exactly what is indexed.
func testModel(t *testing.T, paths ...string) *Model {
	t.Helper()
	var ix indexer.Index
	es := make([]indexer.Entry, len(paths))
	for i, p := range paths {
		es[i] = indexer.NewEntry(p, 0, 0, strings.HasSuffix(p, "/dir"))
	}
	ix.Add(es)

	m := newModel("/r", "", &ix)
	t.Cleanup(m.stop)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	return m
}

// send delivers msg and then runs the resulting commands to completion,
// feeding their messages back, the way the Bubble Tea runtime would. It
// returns true if one of them asked to quit.
func send(m *Model, msg tea.Msg) (quit bool) {
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		msg, queue = queue[0], queue[1:]
		_, cmd := m.Update(msg)
		for _, c := range flatten(cmd) {
			switch out := c().(type) {
			case tea.QuitMsg:
				quit = true
			case tea.BatchMsg:
				for _, inner := range out {
					queue = append(queue, inner())
				}
			case nil:
			default:
				queue = append(queue, out)
			}
		}
	}
	return quit
}

func flatten(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	return []tea.Cmd{cmd}
}

func typeText(m *Model, s string) {
	for _, r := range s {
		send(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func key(m *Model, code rune, mod tea.KeyMod) bool {
	return send(m, tea.KeyPressMsg{Code: code, Mod: mod})
}

func (m *Model) resultPaths() []string {
	ps := make([]string, len(m.results))
	for i := range m.results {
		ps[i] = m.results[i].Entry.Path
	}
	return ps
}

func TestTypingSearches(t *testing.T) {
	m := testModel(t, "/r/main/p.go", "/r/p/main.go", "/r/docs/readme.md")
	typeText(m, "main")

	if m.query != "main" {
		t.Fatalf("query = %q, want main", m.query)
	}
	if got := m.resultPaths(); len(got) != 2 || got[0] != "/r/p/main.go" {
		t.Errorf("results = %q, want /r/p/main.go first and readme excluded", got)
	}
	if m.total != 2 {
		t.Errorf("total = %d, want 2", m.total)
	}
}

func TestSelectionMovesAndClamps(t *testing.T) {
	m := testModel(t, "/r/a1.go", "/r/a2.go", "/r/a3.go")
	typeText(m, "a")

	key(m, tea.KeyDown, 0)
	key(m, tea.KeyDown, 0)
	key(m, tea.KeyDown, 0) // past the end
	if m.cursor != 2 {
		t.Errorf("cursor = %d after moving past the end, want 2", m.cursor)
	}
	key(m, 'k', tea.ModCtrl)
	if m.cursor != 1 {
		t.Errorf("cursor = %d after ctrl+k, want 1", m.cursor)
	}
	key(m, 'j', tea.ModCtrl)
	if m.cursor != 2 {
		t.Errorf("cursor = %d after ctrl+j, want 2", m.cursor)
	}
	key(m, 'k', tea.ModAlt)
	if m.cursor != 1 {
		t.Errorf("cursor = %d after alt+k, want 1", m.cursor)
	}
	key(m, 'p', tea.ModCtrl)
	if m.cursor != 0 {
		t.Errorf("cursor = %d after ctrl+p, want 0", m.cursor)
	}
	key(m, 'n', tea.ModCtrl)
	if m.cursor != 1 {
		t.Errorf("cursor = %d after ctrl+n, want 1", m.cursor)
	}

	typeText(m, "1") // a new query starts at the top again
	if m.cursor != 0 {
		t.Errorf("cursor = %d after typing, want 0", m.cursor)
	}
}

func TestScrollKeepsSelectionVisible(t *testing.T) {
	paths := make([]string, 0, 30)
	for i := range 30 {
		paths = append(paths, "/r/f"+string(rune('a'+i%26))+".go")
	}
	m := testModel(t, paths...)
	typeText(m, "f")

	for range 20 {
		key(m, tea.KeyDown, 0)
	}
	if h := m.listHeight(); m.cursor < m.offset || m.cursor >= m.offset+h {
		t.Errorf("cursor %d outside viewport [%d, %d)", m.cursor, m.offset, m.offset+h)
	}
}

func TestEscStepsBack(t *testing.T) {
	m := testModel(t, "/r/main.go")
	typeText(m, "ma")

	if quit := key(m, tea.KeyEscape, 0); quit || m.query != "" {
		t.Fatalf("first esc: quit=%v query=%q, want the query cleared and no quit", quit, m.query)
	}
	if quit := key(m, tea.KeyEscape, 0); !quit {
		t.Error("second esc on an empty query must quit")
	}
	if m.ExitDir() != "" {
		t.Errorf("ExitDir = %q after esc, want no folder change", m.ExitDir())
	}
}

func TestCtrlEJumps(t *testing.T) {
	m := testModel(t, "/r/src/main.go", "/r/fndr/dir")
	if quit := key(m, 'e', tea.ModCtrl); !quit || m.ExitDir() != "/r" {
		t.Errorf("ctrl+e with nothing selected: quit=%v ExitDir=%q, want the current folder", quit, m.ExitDir())
	}

	m = testModel(t, "/r/src/main.go", "/r/fndr/dir")
	typeText(m, "fndrdir")
	key(m, 'e', tea.ModCtrl)
	if m.ExitDir() != "/r/fndr/dir" {
		t.Errorf("ctrl+e on a folder: ExitDir = %q, want the folder itself", m.ExitDir())
	}

	m = testModel(t, "/r/src/main.go", "/r/fndr/dir")
	typeText(m, "main")
	key(m, 'e', tea.ModCtrl)
	if m.ExitDir() != "/r/src" {
		t.Errorf("ctrl+e on a file: ExitDir = %q, want its folder", m.ExitDir())
	}
}

// TestCtrlEWhileBrowsingStaysPut: browsing, the selection after going up
// rests on the folder just left; ctrl+e must not jump back into it.
func TestCtrlEWhileBrowsingStaysPut(t *testing.T) {
	m, root := browseModel(t, "")
	m.selectName(t, "src")
	key(m, tea.KeyEnter, 0)
	typeText(m, "-") // back up; the selection rests on src
	key(m, 'e', tea.ModCtrl)
	if m.ExitDir() != root {
		t.Errorf("ExitDir = %q, want the folder being browsed %q", m.ExitDir(), root)
	}
}

func TestCtrlCLeavesTheShellWhereItWas(t *testing.T) {
	m := testModel(t, "/r/main.go")
	if quit := key(m, 'c', tea.ModCtrl); !quit || m.ExitDir() != "" {
		t.Errorf("ctrl+c: quit=%v ExitDir=%q, want quit and no folder change", quit, m.ExitDir())
	}
}

func TestEditingKeys(t *testing.T) {
	m := testModel(t, "/r/x")
	typeText(m, "foo bar")

	key(m, tea.KeyBackspace, 0)
	if m.query != "foo ba" {
		t.Errorf("after backspace: %q", m.query)
	}
	key(m, 'w', tea.ModCtrl)
	if m.query != "foo " {
		t.Errorf("after ctrl+w: %q", m.query)
	}
	key(m, 'u', tea.ModCtrl)
	if m.query != "" {
		t.Errorf("after ctrl+u: %q", m.query)
	}

	typeText(m, "отч") // multi-byte input and backspace by rune
	key(m, tea.KeyBackspace, 0)
	if m.query != "от" {
		t.Errorf("Cyrillic backspace: %q, want от", m.query)
	}
}

func TestCtrlChordsAreNotText(t *testing.T) {
	m := testModel(t, "/r/x")
	send(m, tea.KeyPressMsg{Code: 'j', Text: "j", Mod: tea.ModAlt})
	if m.query != "" {
		t.Errorf("alt+j typed %q into the query", m.query)
	}
}

// TestStaleSearchIsDropped checks the race the seq counter exists for: an
// old, slow search answering after the user has typed more must not
// overwrite newer results.
func TestStaleSearchIsDropped(t *testing.T) {
	m := testModel(t, "/r/main.go", "/r/other.go")
	typeText(m, "main")
	want := m.resultPaths()

	m.Update(searchMsg{seq: m.seq - 1, results: []indexer.Result{result("/r/other.go")}, total: 1})
	if got := m.resultPaths(); len(got) != len(want) || got[0] != want[0] {
		t.Errorf("stale reply replaced results: got %q, want %q", got, want)
	}
}

// TestIndexGrowthRefreshesResults covers searching while the crawl is still
// running: a file indexed after the search started must still appear.
func TestIndexGrowthRefreshesResults(t *testing.T) {
	m := testModel(t, "/r/a.go")
	m.crawling = true
	m.crawl = finishedStream(indexer.Stats{Entries: 2, Skipped: 1})
	typeText(m, "b")
	if len(m.results) != 0 {
		t.Fatalf("results = %q before b.go is indexed", m.resultPaths())
	}

	send(m, batchMsg{gen: m.crawlGen, entries: []indexer.Entry{indexer.NewEntry("/r/b.go", 0, 0, false)}})
	if got := m.resultPaths(); len(got) != 1 || got[0] != "/r/b.go" {
		t.Errorf("results = %q after indexing b.go, want it found", got)
	}
	if m.crawling || m.skipped != 1 {
		t.Errorf("crawling=%v skipped=%d, want the crawl reported finished with 1 skipped", m.crawling, m.skipped)
	}
}

// finishedStream is a crawl stream with no batches left: its next read
// reports the crawl as done with stats.
func finishedStream(stats indexer.Stats) *crawlStream {
	s := &crawlStream{
		batches: make(chan []indexer.Entry),
		done:    make(chan crawlDoneMsg, 1),
	}
	close(s.batches)
	s.done <- crawlDoneMsg{stats: stats}
	return s
}

// TestViewFillsScreenExactly is the layout guarantee for the whole screen:
// exactly height lines of exactly width cells, in every state, at every size.
func TestViewFillsScreenExactly(t *testing.T) {
	sizes := [][2]int{{80, 24}, {40, 10}, {20, 5}, {200, 50}, {10, 4}}
	states := map[string]func(*Model){
		"empty query":   func(*Model) {},
		"results":       func(m *Model) { typeText(m, "go") },
		"no matches":    func(m *Model) { typeText(m, "zzz") },
		"long query":    func(m *Model) { typeText(m, strings.Repeat("g", 300)) },
		"while crawled": func(m *Model) { m.crawling = true; m.skipped = 3 },
		"long notice":   func(m *Model) { m.setNotice(strings.Repeat("cannot open ", 30), true) },
	}
	for name, setup := range states {
		for _, sz := range sizes {
			m := testModel(t, "/r/main.go", "/r/cmd/fndr/main.go", "/r/мои/отчёт.go", "/r/dir")
			setup(m)
			m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})

			lines := strings.Split(m.View().Content, "\n")
			if len(lines) != sz[1] {
				t.Errorf("%s at %dx%d: %d lines, want %d", name, sz[0], sz[1], len(lines), sz[1])
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != sz[0] {
					t.Errorf("%s at %dx%d: line %d is %d cells: %q", name, sz[0], sz[1], i, w, ansi.Strip(l))
				}
			}
		}
	}
}

func TestStatusBarIsHonest(t *testing.T) {
	m := testModel(t, "/r/a.go", "/r/b.go")
	m.skipped = 2
	typeText(m, "a")

	status := ansi.Strip(m.viewStatus())
	for _, want := range []string{"LOCAL", "/r", "1/2", "2 unreadable"} {
		if !strings.Contains(status, want) {
			t.Errorf("status %q is missing %q", status, want)
		}
	}
}

func TestHumanCount(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1k", 12_400: "12.4k", 1_250_000: "1.2M"} {
		if got := humanCount(n); got != want {
			t.Errorf("humanCount(%d) = %q, want %q", n, got, want)
		}
	}
}
