package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

func tagModel(t *testing.T) *Model {
	t.Helper()
	return testModel(t, "/r/a/main.go", "/r/a/util.go", "/r/notes.md", "/r/a/dir")
}

func tag(m *Model, text string) {
	typeText(m, text)
	key(m, 't', tea.ModCtrl)
}

func (m *Model) tagTexts() []string {
	ts := make([]string, len(m.tags))
	for i, t := range m.tags {
		ts[i] = t.String()
	}
	return ts
}

// TestTagNarrowsSearch: what is typed becomes a tag on ctrl+t, a chip
// before an empty query, and alone it is a search: every .go file below
// the folder. Typing then searches among them.
func TestTagNarrowsSearch(t *testing.T) {
	m := tagModel(t)
	tag(m, ".GO")
	if m.query != "" || !slices.Equal(m.tagTexts(), []string{".go"}) {
		t.Fatalf("query %q, tags %q; want the query turned into the tag .go", m.query, m.tagTexts())
	}
	if got := m.names(); !slices.Equal(sorted(got), []string{"main.go", "util.go"}) {
		t.Errorf("tag alone: %q, want both .go files", got)
	}
	if line := m.viewPrompt(); !strings.Contains(line, ".go") {
		t.Errorf("no chip on the query line: %q", line)
	}

	typeText(m, "ma")
	if got := m.names(); !slices.Equal(got, []string{"main.go"}) {
		t.Errorf("tag and query: %q, want main.go", got)
	}
	// The text cursor stands after the chip and the query, not over the chip.
	if x, want := m.View().Cursor.X, promptWidth+len(" .go ")+1+len("ma"); x != want {
		t.Errorf("cursor at %d, want %d", x, want)
	}
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

// TestTagAgainTakesItOff: the same tag, in any case, toggles.
func TestTagAgainTakesItOff(t *testing.T) {
	m := tagModel(t)
	tag(m, ".go")
	tag(m, "dirs")
	tag(m, " .Go ")
	if !slices.Equal(m.tagTexts(), []string{"dirs"}) {
		t.Errorf("tags %q, want only dirs left", m.tagTexts())
	}
}

// TestTagsStack: two tags must both match.
func TestTagsStack(t *testing.T) {
	m := tagModel(t)
	tag(m, "a")
	tag(m, "dirs")
	if got := m.names(); !slices.Equal(got, []string{"dir"}) {
		t.Errorf("a + dirs: %q, want the folder a/dir alone", got)
	}
}

// TestBackspaceTakesTagOff: on an empty query, backspace erases the chip
// before the cursor, as it would a letter, instead of going up.
func TestBackspaceTakesTagOff(t *testing.T) {
	m := tagModel(t)
	tag(m, ".go")
	tag(m, "dirs")
	key(m, tea.KeyBackspace, 0)
	if m.cwd != "/r" || !slices.Equal(m.tagTexts(), []string{".go"}) {
		t.Fatalf("cwd %q, tags %q; want /r and .go", m.cwd, m.tagTexts())
	}
	key(m, tea.KeyBackspace, 0)
	if len(m.tags) != 0 || !m.browsing() {
		t.Errorf("tags %q, browsing %v; want none and the folder", m.tagTexts(), m.browsing())
	}
}

// TestEscTakesTagOffFirst: Esc is a step back, the last tag first, and
// leaves what is typed.
func TestEscTakesTagOffFirst(t *testing.T) {
	m := tagModel(t)
	tag(m, ".go")
	typeText(m, "ma")
	if quit := key(m, tea.KeyEscape, 0); quit || len(m.tags) != 0 || m.query != "ma" {
		t.Errorf("quit %v, tags %q, query %q; want the tag off, the query kept", quit, m.tagTexts(), m.query)
	}
}

// TestBadTagSaysWhy: a tag that does not parse stays typed, and the
// status says why; nothing typed, ctrl+t says what a tag is.
func TestBadTagSaysWhy(t *testing.T) {
	m := tagModel(t)
	tag(m, ">1xb")
	if len(m.tags) != 0 || m.query != ">1xb" || !m.noticeErr || !strings.Contains(m.notice, "unit") {
		t.Errorf("tags %q, query %q, notice %q (err %v)", m.tagTexts(), m.query, m.notice, m.noticeErr)
	}

	m = tagModel(t)
	key(m, 't', tea.ModCtrl)
	if len(m.tags) != 0 || !strings.Contains(m.notice, "ctrl+t") {
		t.Errorf("ctrl+t on nothing: tags %q, notice %q", m.tagTexts(), m.notice)
	}
}

// TestRenameDropsTags: a file renamed out of what the tags let through
// must not vanish (it is selected under its new name), so the tags go.
func TestRenameDropsTags(t *testing.T) {
	m, root := browseModel(t, "")
	tag(m, ".go")
	typeText(m, "util")
	key(m, 'r', tea.ModCtrl)
	retype(m, "src/util.txt")
	to := filepath.Join(root, "src", "util.txt")
	if len(m.tags) != 0 || len(m.results) == 0 || m.results[m.cursor].Entry.Path != to {
		t.Errorf("tags %q, results %q; want no tags and util.txt selected", m.tagTexts(), m.resultPaths())
	}
}

// TestTagAloneIsASearch: with a tag and nothing typed, fndr searches,
// everywhere a typed query would make it: what the crawl adds shows up,
// a listing read late does not cover the results, ctrl+e goes where the
// selected file is, and nothing found says so.
func TestTagAloneIsASearch(t *testing.T) {
	t.Run("crawl", func(t *testing.T) {
		m := tagModel(t)
		m.local.crawling = true
		m.local.stream = finishedStream(indexer.Stats{})
		tag(m, ".rs")
		send(m, batchMsg{gen: m.crawlGen, entries: []indexer.Entry{indexer.NewEntry("/r/lib.rs", 0, 0, false)}})
		if got := m.names(); !slices.Equal(got, []string{"lib.rs"}) {
			t.Errorf("after the crawl found lib.rs: %q", got)
		}
	})
	t.Run("crawl still running", func(t *testing.T) {
		// The end of the crawl searches again anyway; this is the search
		// while it runs, hundreds of milliseconds on a real home.
		m := tagModel(t)
		m.local.crawling = true
		m.local.stream = &crawlStream{batches: make(chan []indexer.Entry), done: make(chan crawlDoneMsg, 1)}
		tag(m, ".rs")
		m.batch(&batchMsg{gen: m.local.gen, entries: []indexer.Entry{indexer.NewEntry("/r/lib.rs", 0, 0, false)}})
		if !m.searching {
			t.Error("a batch did not search again: the results stay as they were until the crawl ends")
		}
	})
	t.Run("crawl done", func(t *testing.T) {
		m := tagModel(t)
		m.local.crawling = true
		tag(m, ".rs")
		m.local.ix.Add([]indexer.Entry{indexer.NewEntry("/r/lib.rs", 0, 0, false)})
		send(m, crawlDoneMsg{gen: m.local.gen})
		if got := m.names(); !slices.Equal(got, []string{"lib.rs"}) {
			t.Errorf("after the crawl ended: %q", got)
		}
	})
	t.Run("listing late", func(t *testing.T) {
		m, _ := browseModel(t, "")
		tag(m, ".go")
		send(m, m.load(m.cwd)())
		if got := sorted(m.names()); !slices.Equal(got, []string{"main.go", "util.go"}) {
			t.Errorf("after the folder was read again: %q", got)
		}
	})
	t.Run("jump", func(t *testing.T) {
		m := tagModel(t)
		tag(m, "main")
		key(m, 'e', tea.ModCtrl)
		if m.exitDir != "/r/a" {
			t.Errorf("ctrl+e on a/main.go goes to %q, want its folder /r/a", m.exitDir)
		}
	})
	t.Run("nothing found", func(t *testing.T) {
		m := tagModel(t)
		tag(m, ".rs")
		if v := m.View().Content; !strings.Contains(v, "no matches") {
			t.Error("a tag that finds nothing must say so")
		}
	})
}
