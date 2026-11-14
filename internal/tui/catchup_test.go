package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestFocusCatchesUp: a file another program made while fndr was away is
// found once fndr is focused again, by a catch-up, not a crawl afresh.
func TestFocusCatchesUp(t *testing.T) {
	m, root := browseModel(t, "")
	// In docs: src, untouched, is then a folder the catch-up takes from
	// the index without reading it.
	made := filepath.Join(root, "docs", "fresh.go")
	if err := os.WriteFile(made, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	send(m, tea.FocusMsg{})
	typeText(m, "fresh")
	if !slices.Contains(m.resultPaths(), made) {
		t.Fatalf("results %q: the file made meanwhile is not found", m.resultPaths())
	}
	if m.local.reused == 0 {
		t.Error("the catch-up read every folder again: a crawl, not a refresh")
	}
}

// TestCatchUpNotTooOften: focus back and forth within catchUpEvery
// catches up once.
func TestCatchUpNotTooOften(t *testing.T) {
	m, _ := browseModel(t, "")
	now := time.Now()
	if m.catchUp(now) == nil {
		t.Fatal("no catch-up at all")
	}
	m.local.crawling = false // as if it had ended
	if m.catchUp(now.Add(catchUpEvery/2)) != nil {
		t.Error("caught up again within catchUpEvery")
	}
	if m.catchUp(now.Add(catchUpEvery)) == nil {
		t.Error("no catch-up after catchUpEvery")
	}
}

// TestIdleKeyCatchesUp: where the terminal does not report focus, the
// first key after catchUpIdle catches up; keys typed on do not.
func TestIdleKeyCatchesUp(t *testing.T) {
	m, _ := browseModel(t, "")
	m.lastKey = time.Now().Add(-catchUpIdle - time.Second)
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !m.local.crawling {
		t.Fatal("the first key after a minute did not catch up")
	}
	m.local.crawling = false
	m.caughtUp = time.Time{} // only the idle rule is tested here
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if m.local.crawling {
		t.Error("a key typed on caught up")
	}
}

// TestViewsAskForFocus: fndr asks the terminal to say when it is focused,
// in search and in OIL.
func TestViewsAskForFocus(t *testing.T) {
	m, _ := oilModel(t)
	if !m.View().ReportFocus {
		t.Error("OIL does not ask for focus reports")
	}
	key(m, tea.KeyEscape, 0)
	if !m.View().ReportFocus {
		t.Error("search does not ask for focus reports")
	}
}

// TestCatchUpLeavesACrawl: focus during a crawl (the first of a home,
// seconds long) leaves it be. Restarted as a catch-up of the index half
// built, it would trust folders read in part, their time being the same,
// and what was not read yet would never be found.
func TestCatchUpLeavesACrawl(t *testing.T) {
	m, _ := browseModel(t, "")
	m.startCrawl(m.local) // never run: under way
	gen := m.local.gen
	m.catchUp(time.Now())
	if m.local.gen != gen || m.local.catchUp {
		t.Error("the crawl under way was replaced by a catch-up")
	}
}
