package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// copyPlanned is OIL with the plan to copy src/ (2 files) to lib/ shown.
func copyPlanned(t *testing.T) (m *Model, root string) {
	t.Helper()
	m, root, _ = writeModel(t)
	goTo(t, m, "src/")
	typeText(m, "yyp")
	typeText(m, "cclib/")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	if m.oil.plan == nil {
		t.Fatalf("no plan: %q", m.notice)
	}
	return m, root
}

// pressY presses y without running what it starts: the copy is still to
// come when it returns, as it is for the user, who goes on at once.
func pressY(m *Model) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	return cmd
}

// runAll runs cmd as the runtime would, and says whether fndr quit.
func runAll(m *Model, cmd tea.Cmd) (quit bool) {
	for _, msg := range runCmd(cmd) {
		if send(m, msg) {
			quit = true
		}
	}
	return quit
}

// TestCopyInBackground: y on a plan with a copy leaves OIL at once, the
// copy queued and under way, counted in the status bar; when it ends it is
// in the listing, said, and ctrl+z takes it back.
func TestCopyInBackground(t *testing.T) {
	m, root := copyPlanned(t)
	cmd := pressY(m)
	if m.oil != nil || len(m.tasks) != 1 || m.tasks[0].cancel == nil {
		t.Fatalf("after y: in OIL %v, %d tasks", m.oil != nil, len(m.tasks))
	}
	if exists(filepath.Join(root, "lib")) {
		t.Fatal("copied before its task ran")
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "⟳ copying 0 of 3 · 0 B") {
		t.Errorf("status while copying:\n%s", v)
	}

	if runAll(m, cmd) {
		t.Fatal("quit when the copy ended, asked to by no one")
	}
	if !exists(filepath.Join(root, "lib", "main.go")) || len(m.tasks) != 0 {
		t.Fatalf("after the task: lib there %v, %d tasks", exists(filepath.Join(root, "lib")), len(m.tasks))
	}
	if m.notice != "copied 1" || !m.indexed(filepath.Join(root, "lib", "main.go")) {
		t.Errorf("notice %q, indexed %v", m.notice, m.indexed(filepath.Join(root, "lib", "main.go")))
	}
	if !strings.Contains(strings.Join(m.names(), " "), "lib") {
		t.Errorf("listing %q: lib not in it", m.names())
	}
	ctrlZ(m)
	if !strings.Contains(m.notice, "copy src") {
		t.Errorf("ctrl+z offers %q", m.notice)
	}
}

// TestTaskProgressShown: the status bar counts what the copy did, of how
// much; a folder past the count, or not counted yet, has no total.
func TestTaskProgressShown(t *testing.T) {
	m, _ := copyPlanned(t)
	pressY(m)
	tk := m.tasks[0]
	tk.progress.Entries.Store(1)
	tk.progress.Bytes.Store(1500)
	if s := m.taskStatus(); s != "⟳ copying 1 of 3 · 1.5 KB" {
		t.Errorf("status %q", s)
	}
	tk.total = -1
	if s := m.taskStatus(); s != "⟳ copying 1 · 1.5 KB" {
		t.Errorf("total unknown: %q", s)
	}
}

// TestTasksOneAtATime: a second copy waits for the first, said in the
// status bar, and starts when it ends.
func TestTasksOneAtATime(t *testing.T) {
	m, root := copyPlanned(t)
	first := pressY(m)
	key(m, 'o', tea.ModCtrl)
	goTo(t, m, "docs/")
	typeText(m, "yyp")
	typeText(m, "ccdocs2/")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	second := pressY(m)
	if len(m.tasks) != 2 || m.tasks[1].cancel != nil {
		t.Fatalf("%d tasks, the second started %v", len(m.tasks), len(m.tasks) == 2 && m.tasks[1].cancel != nil)
	}
	if s := m.taskStatus(); !strings.HasSuffix(s, "· 1 waiting") {
		t.Errorf("status %q", s)
	}
	for _, msg := range runCmd(second) {
		// Queued behind one under way, it starts nothing: run again, the
		// first would be copied twice, its second run begun in the journal
		// and never ended there.
		if _, ok := msg.(taskDoneMsg); ok {
			t.Fatal("queuing a second task ran a task")
		}
		send(m, msg)
	}
	runAll(m, first)
	if !exists(filepath.Join(root, "lib")) || len(m.tasks) != 0 || !exists(filepath.Join(root, "docs2")) {
		t.Errorf("after the first: lib %v, docs2 %v, %d tasks", exists(filepath.Join(root, "lib")), exists(filepath.Join(root, "docs2")), len(m.tasks))
	}
}

// TestQuitAsksWhileCopying: quitting with a copy under way asks; esc
// stays, w quits once it ends, s stops it and quits once it has cleaned
// up after itself.
func TestQuitAsksWhileCopying(t *testing.T) {
	m, _ := copyPlanned(t)
	run := pressY(m)
	if key(m, 'c', tea.ModCtrl) || !m.leaving || m.notice != "1 task running:   w wait   s stop   esc stay" {
		t.Fatalf("ctrl+c: leaving %v, notice %q", m.leaving, m.notice)
	}
	if key(m, tea.KeyEscape, 0) || m.leaving {
		t.Fatal("esc did not stay")
	}
	key(m, 'c', tea.ModCtrl)
	typeText(m, "w")
	if !m.quitWhenDone {
		t.Fatal("w does not wait")
	}
	if !runAll(m, run) {
		t.Error("did not quit once the copy ended")
	}

	m, root := copyPlanned(t)
	run = pressY(m)
	stopped := false
	cancel := m.tasks[0].cancel
	m.tasks[0].cancel = func() { stopped = true; cancel() }
	key(m, 'c', tea.ModCtrl)
	typeText(m, "s")
	if !stopped {
		t.Fatal("s did not stop the copy")
	}
	if !runAll(m, run) || exists(filepath.Join(root, "lib")) {
		t.Errorf("after s: lib there %v", exists(filepath.Join(root, "lib")))
	}
}

// TestTicks: one chain of ticks however many start it, going on while a
// task runs and ending with the last; a rename alone starts no task.
func TestTicks(t *testing.T) {
	m, _ := copyPlanned(t)
	if m.tick() == nil || m.tick() != nil {
		t.Error("two chains of ticks")
	}
	if m.progressTicked() != nil {
		t.Error("ticks with no task")
	}
	pressY(m)
	if m.progressTicked() == nil {
		t.Error("no tick while a task runs")
	}

	m2, _, _ := writeModel(t)
	goTo(t, m2, "README.md")
	typeText(m2, "ccA.md")
	key(m2, tea.KeyEscape, 0)
	command(m2, "w")
	pressY(m2)
	if len(m2.tasks) != 0 {
		t.Error("a rename became a task")
	}
}

// TestStopDropsWaiting: s stops the copy under way, and those waiting
// never start: fndr quits with none of them done.
func TestStopDropsWaiting(t *testing.T) {
	m, root := copyPlanned(t)
	first := pressY(m)
	key(m, 'o', tea.ModCtrl)
	goTo(t, m, "docs/")
	typeText(m, "yyp")
	typeText(m, "ccdocs2/")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	pressY(m)
	key(m, 'c', tea.ModCtrl)
	typeText(m, "s")
	if !runAll(m, first) || exists(filepath.Join(root, "docs2")) || len(m.tasks) != 0 {
		t.Errorf("docs2 there %v, %d tasks", exists(filepath.Join(root, "docs2")), len(m.tasks))
	}
}

// TestPasteDropsLeaving: a paste is typing, not the answer to quit's
// question: its w does not set fndr to quit.
func TestPasteDropsLeaving(t *testing.T) {
	m, _ := copyPlanned(t)
	pressY(m)
	key(m, 'c', tea.ModCtrl)
	send(m, tea.PasteMsg{Content: "w"})
	if m.leaving || m.quitWhenDone {
		t.Errorf("leaving %v, quitWhenDone %v", m.leaving, m.quitWhenDone)
	}
}
