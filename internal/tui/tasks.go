package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// A plan with copies can take minutes: a folder of photos, a disk image.
// fndr never makes the user wait for one: y queues it as a task and
// leaves OIL at once, and the user goes on searching, opening, renaming.
// The plan goes whole, one task: its renames come after its copies, which
// read what they copy before it is renamed. Tasks run one at a time, in
// the order given: two copies at once would share the disk, and on a hard
// or an external disk be slower than one after the other.
//
// A task is written down in the journal like any change (undo.go): a
// crash in the middle is put right at the next start. The status bar
// counts it while it runs; what it did is said when it ends, and ctrl+z
// takes it back. Quitting while one runs asks first: w waits for it, s
// stops it, anything else stays.

// task is a plan with copies, queued or under way.
type task struct {
	id    int
	dir   string
	plan  mutation.Plan
	total int64 // entries its copies hold, or -1 if not known (see copyTotal)

	progress *mutation.Progress // what it copied so far, counted by the copy
	cancel   context.CancelFunc // nil until it starts
	stopping bool               // ctrl+x stopped it; its end is on its way

	// bytes is how many bytes its copies hold, files' contents, as the
	// copy counts them: 0 until the task has summed them (it does, off
	// the UI goroutine, before it copies). A single large file is the
	// longest copy there is, and counted by entries it is "0 of 1" until
	// the end: its bar goes by bytes.
	bytes atomic.Int64
}

// taskDoneMsg is a task ended, as an OIL write ends.
type taskDoneMsg struct {
	id      int
	written oilWrittenMsg
}

// progressEvery is how often the screen is drawn again while a task runs:
// often enough to look alive, rarely enough to cost nothing.
const progressEvery = 100 * time.Millisecond

// progressTickMsg asks for the screen to be drawn again.
type progressTickMsg struct{}

// tick starts the ticks, unless they are coming already: one chain of
// them, however many tasks start while it runs.
func (m *Model) tick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return tea.Tick(m.tickEvery, func(time.Time) tea.Msg { return progressTickMsg{} })
}

// progressTicked keeps the ticks coming while a task runs; each one ends
// in a new frame, with the numbers as they are now.
func (m *Model) progressTicked() tea.Cmd {
	m.ticking = false
	if len(m.tasks) == 0 {
		return nil
	}
	return m.tick()
}

// hasCopies reports whether plan copies anything: a task, not a write.
func hasCopies(plan mutation.Plan) bool {
	for _, op := range plan {
		if op.Kind == mutation.OpCopy {
			return true
		}
	}
	return false
}

// queueTask makes the plan shown a task, leaves OIL, and starts the task
// if none runs. :wq queues it and quits, which asks about it first.
func (m *Model) queueTask() tea.Cmd {
	st := m.oil
	m.taskSeq++
	t := &task{id: m.taskSeq, dir: st.dir, plan: st.plan, total: st.copyTotal(), progress: new(mutation.Progress)}
	m.tasks = append(m.tasks, t)
	quit := st.quitAfter
	st.plan, st.wrote = nil, true // written, as far as the buffer goes: read again from the disk
	// The ghost of the first copy into this folder is selected: what was
	// asked for is in sight while it is made.
	var reveal string
	for _, to := range t.targets() {
		if filepath.Dir(to.path) == st.dir {
			reveal = filepath.Base(to.path)
			break
		}
	}
	leave := m.leaveOil(reveal)
	m.setNotice("copying in the background", false)
	cmds := []tea.Cmd{leave, m.startTask()}
	if quit {
		cmds = append(cmds, m.quit())
	}
	return tea.Batch(cmds...)
}

// startTask starts the first task queued, unless it runs already.
func (m *Model) startTask() tea.Cmd {
	if len(m.tasks) == 0 || m.tasks[0].cancel != nil {
		return nil
	}
	t := m.tasks[0]
	ctx, cancel := context.WithCancel(m.ctx)
	t.cancel = cancel
	run := applyPlan(ctx, t.dir, t.plan, false, m.local.ix.Snapshot(), m.prune, t.progress, m.journaled())
	id := t.id
	done := func() tea.Msg {
		t.bytes.Store(t.size())
		w, _ := run().(oilWrittenMsg) //nolint:errcheck // applyPlan says nothing else
		return taskDoneMsg{id: id, written: w}
	}
	return tea.Batch(done, m.tick())
}

// taskDone takes in a task ended: the index learns what it made, the
// journal keeps it for ctrl+z, the status bar says what it did, the
// listing shows it, and the next task starts. Quitting, asked to wait for
// the tasks, quits once the last one ends.
func (m *Model) taskDone(msg *taskDoneMsg) tea.Cmd {
	if len(m.tasks) == 0 || m.tasks[0].id != msg.id {
		return nil // cannot happen: tasks end in the order they start
	}
	t, w := m.tasks[0], msg.written
	m.tasks = m.tasks[1:]
	t.cancel() // its context's resources: the task is over either way
	m.updateIndexes(w.remove, w.add)
	switch {
	case errors.Is(w.err, context.Canceled):
		// Asked for, so not red: the copy cut short left nothing behind.
		m.setNotice(fmt.Sprintf("stopped: %d of %d done, nothing half copied", len(w.done), len(t.plan)), false)
	case w.err != nil:
		m.setNotice(fmt.Sprintf("%d of %d done: %v", len(w.done), len(t.plan), w.err), true)
	default:
		m.setNotice(summary(w.done), false)
	}
	cmds := []tea.Cmd{m.relearn(w.dir, w.done), m.record(w.batch, w.steps), m.startTask()}
	if m.oil == nil {
		// The listing, from the disk: its ghost gone, what it made there.
		// The ghost under the cursor stays under it, the real row now; one
		// the user moved away from is not taken back to.
		if len(w.done) > 0 && len(m.results) > 0 && t.makes(m.results[m.cursor].Entry.Path) {
			m.reveal = m.results[m.cursor].Entry.Path
		}
		cmds = append(cmds, m.load(m.cwd))
	}
	if m.quitWhenDone && len(m.tasks) == 0 {
		m.leaveAnyway = true
		cmds = append(cmds, m.quit())
	}
	return tea.Batch(cmds...)
}

// taskStatus is the status bar's word on the tasks: what the one under way
// copied so far, and how many wait.
func (m *Model) taskStatus() string {
	if len(m.tasks) == 0 {
		return ""
	}
	t := m.tasks[0]
	done := t.progress.Entries.Load()
	count := fmt.Sprint(done)
	if t.total >= 0 {
		count = fmt.Sprintf("%d of %d", min(done, t.total), t.total)
	}
	size := humanSize(t.progress.Bytes.Load())
	if all := t.bytes.Load(); all > 0 {
		size += " of " + humanSize(all)
	}
	text := "⟳ copying " + count + " · " + size
	if waiting := len(m.tasks) - 1; waiting > 0 {
		text += fmt.Sprintf(" · %d waiting", waiting)
	}
	return text
}

// size sums what t's copies hold: the sizes of the files in what they copy,
// read from the disk (only names and sizes: nothing is opened).
func (t *task) size() int64 {
	var n int64
	for _, op := range t.plan {
		if op.Kind != mutation.OpCopy {
			continue
		}
		_ = filepath.WalkDir(mutation.Target(t.dir, op.From), func(_ string, d fs.DirEntry, err error) error { //nolint:errcheck // what cannot be read is not counted
			if err == nil && d.Type().IsRegular() {
				if info, ierr := d.Info(); ierr == nil {
					n += info.Size()
				}
			}
			return nil
		})
	}
	return n
}

// stopTask is ctrl+x: it stops the copy whose ghost is selected. One under
// way is cancelled, and ends as stopped (taskDone), its half copy removed
// by mutation; one waiting is dropped from the queue, never started.
func (m *Model) stopTask() tea.Cmd {
	var t *task
	if len(m.results) > 0 {
		t = m.ghostOf(m.results[m.cursor].Entry.Path)
	}
	switch {
	case t == nil && len(m.tasks) == 0:
		m.setNotice("no copy to stop", false)
	case t == nil:
		m.setNotice("ctrl+x stops a copy: select its row", false)
	case t.cancel == nil:
		m.tasks = slices.DeleteFunc(m.tasks, func(o *task) bool { return o == t })
		m.setNotice("dropped a copy that had not started", false)
		return m.load(m.cwd) // its ghost gone
	default:
		if !t.stopping { // once: pressed again, it is still stopping, and says so
			t.stopping = true
			t.cancel()
		}
		m.setNotice("stopping…", false)
	}
	return nil
}

// askLeave is quit with tasks running: it asks first (leaveKey answers).
func (m *Model) askLeave() {
	m.leaving = true
	n := "1 task"
	if len(m.tasks) > 1 {
		n = fmt.Sprintf("%d tasks", len(m.tasks))
	}
	m.setNotice(n+" running:   w wait   s stop   esc stay", false)
}

// leaveKey answers the question of askLeave: w quits once the tasks end;
// s stops them, and quits once the one under way has cleaned up after
// itself (its half copy removed); anything else stays.
func (m *Model) leaveKey(msg tea.KeyPressMsg) tea.Cmd {
	m.leaving = false
	switch msg.String() {
	case "w":
		m.quitWhenDone = true
		m.setNotice("quitting once the copy is done…", false)
	case "s":
		m.quitWhenDone = true
		m.tasks = m.tasks[:1] // those waiting never start
		if c := m.tasks[0].cancel; c != nil {
			c()
		}
		m.setNotice("stopping…", false)
	}
	return nil
}

// copyTotal is how many entries the plan's copies hold, each folder with
// all below it, or -1 while a folder is not counted yet, or holds more
// than is counted.
func (st *oilState) copyTotal() int64 {
	var total int64
	for _, op := range st.plan {
		if op.Kind != mutation.OpCopy {
			continue
		}
		if !st.isDir(op.From) {
			total++
			continue
		}
		n, ok := st.trashCounts[op.From]
		if !ok || n >= trashLimit {
			return -1
		}
		total += int64(n) + 1 // what it holds, and itself
	}
	return total
}

// isDir reports whether name, as a step of the plan names it, is a folder.
func (st *oilState) isDir(name string) bool {
	for i, it := range st.items {
		if st.entries[i].IsDir && (it.Name == name+"/" || it.Name == name) {
			return true
		}
	}
	return false
}
