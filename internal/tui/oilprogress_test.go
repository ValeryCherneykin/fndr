package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// copyPlanned is OIL with the plan to copy src/ (2 files) to lib/ shown.
func copyPlanned(t *testing.T) *Model {
	t.Helper()
	m, _, _ := writeModel(t)
	goTo(t, m, "src/")
	typeText(m, "yyp")
	typeText(m, "cclib/")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	if m.oil.plan == nil {
		t.Fatalf("no plan: %q", m.notice)
	}
	return m
}

// TestCopyProgressShown: while copying, the plan box shows how far, of
// how much, in place of its keys; a folder not counted yet has no total.
func TestCopyProgressShown(t *testing.T) {
	m := copyPlanned(t)
	st := m.oil
	st.applying, st.progress = true, new(mutation.Progress)
	st.progress.Entries.Store(1)
	st.progress.Bytes.Store(1500)
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "copying 1 of 3 · 1.5 KB") || !strings.Contains(v, "[esc] stop") || strings.Contains(v, "[y]") {
		t.Errorf("box while copying:\n%s", v)
	}
	st.trashCounts["src"] = trashLimit // counted up to the limit: more, not this many
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "copying 1 · 1.5 KB") {
		t.Errorf("box with a folder past the count:\n%s", v)
	}
	delete(st.trashCounts, "src")
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "copying 1 · 1.5 KB") {
		t.Errorf("box with the total unknown:\n%s", v)
	}
}

// TestCopyEscStops: esc while copying stops it, once, and says so; what
// comes back is not an error.
func TestCopyEscStops(t *testing.T) {
	m := copyPlanned(t)
	st := m.oil
	stops := 0
	st.applying, st.progress, st.stop = true, new(mutation.Progress), func() { stops++ }
	key(m, tea.KeyEscape, 0)
	key(m, tea.KeyEscape, 0)
	if stops != 1 || !st.stopping || st.plan == nil {
		t.Fatalf("esc: %d stops, stopping %v, plan %v", stops, st.stopping, st.plan)
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "stopping…") {
		t.Errorf("box while stopping:\n%s", v)
	}
	send(m, oilWrittenMsg{dir: st.dir, err: fmt.Errorf("copy: %w", context.Canceled)})
	if m.notice != "stopped: 0 of 1 done, nothing half copied" || m.noticeErr || m.oil.plan != nil || m.oil.stopping {
		t.Errorf("after: notice %q (error %v), plan %v", m.notice, m.noticeErr, m.oil.plan)
	}
	if stops != 2 {
		t.Errorf("the context was not released at the end: %d stops", stops)
	}
}

// TestProgressTicksWhileApplying: ticks go on while the plan is carried
// out and stop with it; a plan without copies starts none.
func TestProgressTicksWhileApplying(t *testing.T) {
	m := copyPlanned(t)
	if m.progressTicked() != nil {
		t.Error("a tick before y")
	}
	m.oil.applying = true
	if m.progressTicked() == nil {
		t.Error("no tick while applying")
	}

	m2, _, _ := writeModel(t)
	goTo(t, m2, "README.md")
	typeText(m2, "ccA.md")
	key(m2, tea.KeyEscape, 0)
	command(m2, "w")
	if _, batch := m2.confirmKey(tea.KeyPressMsg{Code: 'y', Text: "y"})().(tea.BatchMsg); batch {
		t.Error("a rename started progress ticks")
	}
}
