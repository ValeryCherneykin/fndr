package tui

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestGhost: a copy under way is a row of its folder's listing before it
// is there, selected, dimmed, saying how far; enter and ctrl+r on it say
// there is nothing yet; when it ends the real row takes its place, under
// the cursor, once.
func TestGhost(t *testing.T) {
	m, root := copyPlanned(t)
	run := pressY(m)
	send(m, m.load(m.cwd)()) // the listing, read; the copy not yet run
	lib := filepath.Join(root, "lib")
	if len(m.results) == 0 || m.results[m.cursor].Entry.Path != lib {
		t.Fatalf("selected %q, want the ghost of lib", m.names())
	}
	m.tasks[0].progress.Entries.Store(1)
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "███░░░░░░░   33%") {
		t.Errorf("the ghost does not say how far:\n%s", v)
	}
	m.tasks[0].bytes.Store(1000) // summed: the bar goes by bytes, a big file not stuck at 0%
	m.tasks[0].progress.Bytes.Store(620)
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "██████░░░░   62%") || !strings.Contains(v, "620 B of 1 KB") {
		t.Errorf("by bytes:\n%s", v)
	}
	m.tasks[0].bytes.Store(0)
	m.tasks[0].total = -1 // a folder not counted yet: no bar, no division by it
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "copying 620 B") {
		t.Errorf("the ghost of a copy of unknown size:\n%s", v)
	}
	key(m, tea.KeyEnter, 0)
	if m.notice != "still copying: there is nothing there yet" {
		t.Errorf("enter on the ghost: %q", m.notice)
	}
	key(m, 'r', tea.ModCtrl)
	if m.rename != nil || m.notice != "still copying: there is nothing there yet" {
		t.Errorf("ctrl+r on the ghost: rename %v, %q", m.rename != nil, m.notice)
	}

	runAll(m, run)
	n := 0
	for _, r := range m.results {
		if r.Entry.Path == lib {
			n++
		}
	}
	if n != 1 || m.results[m.cursor].Entry.Path != lib || m.ghostOf(lib) != nil {
		t.Errorf("after: lib %d times, selected %q", n, m.results[m.cursor].Entry.Path)
	}
}

// TestGhostWhereItGoes: a ghost is in its folder's listing only; one
// waiting for the copy before it says so.
func TestGhostWhereItGoes(t *testing.T) {
	m, root := copyPlanned(t)
	pressY(m)
	key(m, 'o', tea.ModCtrl)
	goTo(t, m, "docs/")
	typeText(m, "yyp")
	typeText(m, "ccdocs2/")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	pressY(m)
	send(m, m.load(m.cwd)())
	if a := ansi.Strip(m.ghostAside(filepath.Join(root, "docs2"))); a != "waiting" {
		t.Errorf("the second ghost says %q", a)
	}
	runAll(m, m.chdir(filepath.Join(root, "src"), ""))
	if len(m.results) == 0 {
		t.Fatal("src not listed")
	}
	for _, r := range m.results {
		if m.ghostOf(r.Entry.Path) != nil {
			t.Errorf("a ghost in src: %s", r.Entry.Path)
		}
	}
}

// TestGhostSelectedBelowHabits: the ghost selected after y is the ghost,
// below the habits shown above the folder, not the first row.
func TestGhostSelectedBelowHabits(t *testing.T) {
	m, root := copyPlanned(t)
	withHistory(t, m)
	now := time.Now()
	m.hist.Visit(filepath.Join(root, "docs"), now)
	m.hist.Visit(filepath.Join(root, "docs"), now)
	pressY(m)
	send(m, m.load(m.cwd)())
	if m.habits != 1 || m.results[m.cursor].Entry.Path != filepath.Join(root, "lib") {
		t.Errorf("%d habits, selected %q", m.habits, m.results[m.cursor].Entry.Path)
	}
}

// TestTaskSize: a task sums the files its copies hold, as the copy counts
// them: src/ holds two files of one byte.
func TestTaskSize(t *testing.T) {
	m, _ := copyPlanned(t)
	run := pressY(m)
	tk := m.tasks[0]
	if n := tk.size(); n != 2 {
		t.Errorf("size %d, want 2", n)
	}
	runAll(m, run)
	if n := tk.bytes.Load(); n != 2 {
		t.Errorf("the task did not sum its bytes before copying: %d", n)
	}
}

// TestStopUnderWay: ctrl+x on the ghost of a copy under way stops it,
// once; the ghost says so; it ends as stopped, nothing of it left, its
// ghost gone from the listing.
func TestStopUnderWay(t *testing.T) {
	m, root := copyPlanned(t)
	run := pressY(m)
	send(m, m.load(m.cwd)())
	stops := 0
	cancel := m.tasks[0].cancel
	m.tasks[0].cancel = func() { stops++; cancel() }
	key(m, 'x', tea.ModCtrl)
	key(m, 'x', tea.ModCtrl)
	if stops != 1 || m.notice != "stopping…" {
		t.Fatalf("%d stops, notice %q", stops, m.notice)
	}
	if a := ansi.Strip(m.ghostAside(filepath.Join(root, "lib"))); a != "stopping…" {
		t.Errorf("the ghost says %q", a)
	}
	runAll(m, run)
	if m.notice != "stopped: 0 of 1 done, nothing half copied" || m.noticeErr {
		t.Errorf("notice %q", m.notice)
	}
	if exists(filepath.Join(root, "lib")) || len(hiddenIn(t, root)) > 0 {
		t.Errorf("left: lib %v, hidden %q", exists(filepath.Join(root, "lib")), hiddenIn(t, root))
	}
	for _, r := range m.results {
		if r.Entry.Path == filepath.Join(root, "lib") {
			t.Error("the ghost is still listed")
		}
	}
}

// TestStopWaiting: ctrl+x on the ghost of a copy waiting drops it from the
// queue: it never starts.
func TestStopWaiting(t *testing.T) {
	m, root := copyPlanned(t)
	first := pressY(m)
	key(m, 'o', tea.ModCtrl)
	goTo(t, m, "docs/")
	typeText(m, "yyp")
	typeText(m, "ccdocs2/")
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	pressY(m)
	send(m, m.load(m.cwd)())
	docs2 := filepath.Join(root, "docs2")
	for m.results[m.cursor].Entry.Path != docs2 {
		key(m, tea.KeyDown, 0)
	}
	key(m, 'x', tea.ModCtrl)
	if len(m.tasks) != 1 || m.notice != "dropped a copy that had not started" {
		t.Fatalf("%d tasks, notice %q", len(m.tasks), m.notice)
	}
	for _, r := range m.results {
		if r.Entry.Path == docs2 {
			t.Fatal("the ghost of the copy dropped is still listed")
		}
	}
	runAll(m, first)
	if exists(docs2) || !exists(filepath.Join(root, "lib")) {
		t.Errorf("docs2 %v, lib %v", exists(docs2), exists(filepath.Join(root, "lib")))
	}
}

// TestStopNotOnGhost: ctrl+x elsewhere says how it is used, or that there
// is nothing to stop; it stops nothing.
func TestStopNotOnGhost(t *testing.T) {
	m, _, _ := writeModel(t)
	key(m, tea.KeyEscape, 0) // out of OIL
	key(m, 'x', tea.ModCtrl)
	if m.notice != "no copy to stop" {
		t.Errorf("no tasks: %q", m.notice)
	}
	m, _ = copyPlanned(t)
	pressY(m)
	send(m, m.load(m.cwd)())
	key(m, tea.KeyDown, 0) // off the ghost
	key(m, 'x', tea.ModCtrl)
	if m.notice != "ctrl+x stops a copy: select its row" || m.tasks[0].stopping {
		t.Errorf("off the ghost: %q", m.notice)
	}
}

// hiddenIn returns the names fndr hides its work under, left below dir.
func hiddenIn(t *testing.T, dir string) []string {
	t.Helper()
	var left []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error { //nolint:errcheck // test
		if err == nil && strings.HasPrefix(d.Name(), ".fndr-") {
			left = append(left, p)
		}
		return nil
	})
	return left
}
