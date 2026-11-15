package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/frecency"
	"github.com/ValeryCherneykin/fndr/internal/journal"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// ctrl+z takes back the last change fndr made to the disk: an OIL write
// or a ctrl+r, whole, however long ago, by the steps mutation.Run kept
// (mutation.Undo says what is safe). Every batch of steps goes into a
// journal on the disk, next to the history, so it outlives fndr: undo
// what was done yesterday. With history = off nothing is written there;
// batches are kept while fndr runs, and undo works until it quits.
//
// ctrl+z asks first, naming what it takes back and when: it may be a
// batch of another day, and the user must know which.
//
// A change is written down twice: what it is about to do before it starts
// (begin: the plan, every name it will use, mutation.Intent), what it did
// once it ends (batch). A begin with no batch is a change cut off by a
// crash; the next fndr to start makes sense of it on the disk
// (mutation.Recover), writes its batch, and says so: ctrl+z takes it back
// like any other.

// batch is a change carried out: one write, one rename.
type batch struct {
	ID    string          `json:"id"`
	Time  time.Time       `json:"time"`
	Steps []mutation.Step `json:"steps"`
}

// begun is a change about to start, as it will run.
type begun struct {
	ID     string          `json:"id"`
	Time   time.Time       `json:"time"`
	Intent mutation.Intent `json:"intent"`
}

// undoing is an undo about to start: the batch, and the process that
// undoes it, whose temporary names a crash may leave (mutation.RecoverUndo).
type undoing struct {
	ID  string `json:"id"`
	PID int    `json:"pid"`
}

// record is one record of the journal, one of: a change begun, a batch
// done, an undo begun, a batch undone, an undo cut off and put right (its
// batch not undone: ctrl+z finishes it).
type record struct {
	Begin     *begun   `json:"begin,omitempty"`
	Batch     *batch   `json:"batch,omitempty"`
	Undoing   *undoing `json:"undoing,omitempty"`
	Undone    string   `json:"undone,omitempty"`
	Recovered string   `json:"recovered,omitempty"`
}

// undoState is a batch ctrl+z offered to take back, for y to do it.
type undoState struct{ batch batch }

type (
	// undoFoundMsg is the last batch, read from the journal; ok false if
	// there is none to undo.
	undoFoundMsg struct {
		batch batch
		ok    bool
		err   error
	}
	// undoneMsg is a batch taken back: undone of its steps, err naming
	// the others.
	undoneMsg struct {
		batch  batch
		undone int
		err    error
	}
	// recordedMsg says a batch could not be written to the journal.
	recordedMsg struct{ err error }
	// recoveredMsg is what the changes cut off last time came to.
	recoveredMsg struct {
		cut, done, total, fixed int // cut: the changes made sense of
		undos                   int // undos cut off and put right: ctrl+z finishes them
		err                     error
	}
)

// journalPath is where batches are kept: beside the history.
func journalPath() (string, error) {
	hist, err := frecency.DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(hist), "journal"), nil
}

// journaled is where a change is written down: the journal ("" for
// none) and the change's ID, for its begin and its batch.
type journaled struct{ path, id string }

// journal names a change about to start, to be written down.
func (m *Model) journaled() journaled { return journaled{path: m.journal, id: m.newBatchID()} }

// newBatchID names a change about to start, for its begin and its batch:
// unique across fndr running at once, in other panes.
func (m *Model) newBatchID() string {
	m.batchSeq++
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(os.Getpid()) + "-" + strconv.Itoa(m.batchSeq)
}

// begin writes down in, the change id is about to make, before it starts:
// called from the change's command, as it writes and syncs to the disk.
// With no journal, nothing: a crash then loses only the undo.
func (j journaled) begin(in mutation.Intent) error {
	if j.path == "" {
		return nil
	}
	return appendRecord(j.path, record{Begin: &begun{ID: j.id, Time: time.Now(), Intent: in}})
}

// record keeps the steps of the change id carried out, for ctrl+z: in the
// journal, in a command (a disk write, synced), or in memory without one.
// A change that did nothing is written down too, if it was begun: its
// begin is then known to be over, not cut off.
func (m *Model) record(id string, steps []mutation.Step) tea.Cmd {
	if id == "" || (len(steps) == 0 && m.journal == "") {
		return nil
	}
	b := batch{ID: id, Time: time.Now(), Steps: steps}
	if m.journal == "" {
		m.batches = append(m.batches, b)
		return nil
	}
	path := m.journal
	return func() tea.Msg { return recordedMsg{err: appendRecord(path, record{Batch: &b})} }
}

// recoverJournal makes sense of the changes the journal at path has begun
// and not ended: cut off by a crash. One whose fndr is still running, in
// another pane, is under way, not cut off, and left to it.
func recoverJournal(path string) tea.Cmd {
	return func() tea.Msg {
		recs, err := journal.Read(path)
		if err != nil {
			return recoveredMsg{err: err}
		}
		var msg recoveredMsg
		for _, b := range cutOff(recs) {
			if alive(b.Intent.PID) {
				continue
			}
			trash, err := mutation.HomeTrash()
			if err != nil {
				return recoveredMsg{err: err}
			}
			steps, fixed, rerr := mutation.Recover(b.Intent, trash)
			msg.cut++
			msg.done, msg.total, msg.fixed = msg.done+len(steps), msg.total+len(b.Intent.Plan), msg.fixed+fixed
			if err := appendRecord(path, record{Batch: &batch{ID: b.ID, Time: b.Time, Steps: steps}}); err != nil && rerr == nil {
				rerr = err
			}
			if rerr != nil {
				msg.err = rerr
			}
		}
		for _, u := range undosCutOff(recs) {
			if alive(u.PID) {
				continue
			}
			fixed, rerr := mutation.RecoverUndo(u.steps, u.PID)
			msg.cut, msg.undos, msg.fixed = msg.cut+1, msg.undos+1, msg.fixed+fixed
			if err := appendRecord(path, record{Recovered: u.ID}); err != nil && rerr == nil {
				rerr = err
			}
			if rerr != nil {
				msg.err = rerr
			}
		}
		return msg
	}
}

// undoCut is an undo begun and never ended, with its batch's steps.
type undoCut struct {
	undoing
	steps []mutation.Step
}

// undosCutOff returns the undos recs begin and never end, in order: an
// undo of a batch is over once undone or put right, and a later undo of
// the same batch (ctrl+z again) is a new one, cut off or not.
func undosCutOff(recs [][]byte) []undoCut {
	steps := map[string][]mutation.Step{}
	open := map[string]int{} // batch ID → pid of the undo under way
	var order []string
	for _, data := range recs {
		var r record
		if json.Unmarshal(data, &r) != nil {
			continue
		}
		switch {
		case r.Batch != nil:
			steps[r.Batch.ID] = r.Batch.Steps
		case r.Undoing != nil:
			open[r.Undoing.ID] = r.Undoing.PID
			order = append(order, r.Undoing.ID)
		case r.Undone != "":
			delete(open, r.Undone)
		case r.Recovered != "":
			delete(open, r.Recovered)
		}
	}
	var out []undoCut
	for _, id := range order {
		if pid, ok := open[id]; ok {
			out = append(out, undoCut{undoing{ID: id, PID: pid}, steps[id]})
			delete(open, id) // once, however many times it was begun
		}
	}
	return out
}

// cutOff returns the changes recs begin and never end.
func cutOff(recs [][]byte) []begun {
	var begins []begun
	ended := map[string]bool{}
	for _, data := range recs {
		var r record
		if json.Unmarshal(data, &r) != nil {
			continue
		}
		switch {
		case r.Begin != nil:
			begins = append(begins, *r.Begin)
		case r.Batch != nil:
			ended[r.Batch.ID] = true
		}
	}
	var out []begun
	for _, b := range begins {
		if !ended[b.ID] {
			out = append(out, b)
		}
	}
	return out
}

// alive reports whether process pid is running: signal 0 is not sent, only
// checked. EPERM is a process of another user's: running.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// recovered says what the changes cut off last time came to, and catches
// the index and the listing up with the disk: read at start, they may hold
// what recovery took away (a copy cut off, under its hidden name). Nothing
// done, nothing said; nothing cut off, nothing read again.
func (m *Model) recovered(msg *recoveredMsg) tea.Cmd {
	if msg.cut == 0 {
		return nil
	}
	switch {
	case msg.err != nil:
		m.setNotice(safeText("cut off last time, and not all put right: "+msg.err.Error()), true)
	case msg.done == 0 && msg.fixed == 0 && msg.undos == 0:
	default:
		text := fmt.Sprintf("cut off last time: %d of %d done", msg.done, msg.total)
		if msg.total == 0 { // only an undo was
			text = "an undo was cut off last time"
		}
		if msg.fixed > 0 {
			text += fmt.Sprintf(", %d put back from a hidden name", msg.fixed)
		}
		switch {
		case msg.undos > 0:
			text += " — ctrl+z finishes the undo"
		case msg.done > 0:
			text += " — ctrl+z takes it back"
		}
		m.setNotice(text, false)
	}
	m.caughtUp = time.Time{}
	return m.catchUp(time.Now())
}

// appendRecord writes rec to the journal at path, its folder made if new.
func appendRecord(path string, rec record) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return journal.Append(path, data)
}

// lastBatch returns the newest batch of recs not undone yet. A record it
// cannot read, written by a newer fndr, say, is passed over, not a reason
// to lose the rest.
func lastBatch(recs [][]byte) (batch, bool) {
	undone := map[string]bool{}
	var batches []batch
	for _, data := range recs {
		var r record
		if json.Unmarshal(data, &r) != nil {
			continue
		}
		switch {
		case r.Batch != nil && len(r.Batch.Steps) > 0: // one that did nothing is nothing to undo
			batches = append(batches, *r.Batch)
		case r.Undone != "":
			undone[r.Undone] = true
		}
	}
	for i := len(batches) - 1; i >= 0; i-- {
		if !undone[batches[i].ID] {
			return batches[i], true
		}
	}
	return batch{}, false
}

// startUndo looks for the batch to take back and offers it.
func (m *Model) startUndo() tea.Cmd {
	if m.journal == "" {
		recs := make([][]byte, 0, len(m.batches))
		for _, b := range m.batches {
			data, _ := json.Marshal(record{Batch: &b}) //nolint:errcheck // a batch always marshals
			recs = append(recs, data)
		}
		for _, id := range m.undoneIDs {
			data, _ := json.Marshal(record{Undone: id}) //nolint:errcheck // as above
			recs = append(recs, data)
		}
		b, ok := lastBatch(recs)
		return func() tea.Msg { return undoFoundMsg{batch: b, ok: ok} }
	}
	path := m.journal
	return func() tea.Msg {
		recs, err := journal.Read(path)
		if err != nil {
			return undoFoundMsg{err: err}
		}
		b, ok := lastBatch(recs)
		return undoFoundMsg{batch: b, ok: ok}
	}
}

// undoFound offers the batch found, or says there is none.
func (m *Model) undoFound(msg *undoFoundMsg) {
	switch {
	case msg.err != nil:
		m.setNotice("undo: "+msg.err.Error(), true)
	case !msg.ok:
		m.setNotice("nothing to undo", false)
	default:
		m.undoing = &undoState{batch: msg.batch}
		m.setNotice(safeText(fmt.Sprintf("undo %s (%s)?", m.describe(msg.batch.Steps), when(msg.batch.Time, time.Now())))+"   y yes   esc no", false)
	}
}

// undoKey answers the offer: y takes the batch back, anything else drops
// it.
func (m *Model) undoKey(msg tea.KeyPressMsg) tea.Cmd {
	st := m.undoing
	m.undoing = nil
	if msg.String() != "y" {
		return nil
	}
	m.setNotice("undoing…", false)
	path, b := m.journal, st.batch
	return func() tea.Msg {
		trash, err := mutation.HomeTrash()
		if err != nil {
			return undoneMsg{batch: b, err: err}
		}
		if path != "" {
			// Written down first, as a change is: an undo cut off leaves
			// hidden names that only this says where to find.
			if err := appendRecord(path, record{Undoing: &undoing{ID: b.ID, PID: os.Getpid()}}); err != nil {
				return undoneMsg{batch: b, err: fmt.Errorf("journal: %w", err)}
			}
		}
		n, err := mutation.Undo(b.Steps, trash)
		// Marked undone even in part: what could not go back has changed
		// since, and a second ctrl+z would only find it changed again;
		// the one before it is what is offered next.
		if path != "" {
			if jerr := appendRecord(path, record{Undone: b.ID}); jerr != nil && err == nil {
				err = fmt.Errorf("undone, but not written down: %w", jerr)
			}
		}
		return undoneMsg{batch: b, undone: n, err: err}
	}
}

// undone says what went back, and catches the index up with it: the same
// refresh as when fndr is focused again, which reads the folders changed.
func (m *Model) undone(msg *undoneMsg) tea.Cmd {
	if m.journal == "" {
		m.undoneIDs = append(m.undoneIDs, msg.batch.ID)
	}
	total := len(msg.batch.Steps)
	if msg.err != nil {
		m.setNotice(safeText(fmt.Sprintf("undone %d of %d: %v", msg.undone, total, msg.err)), true)
	} else {
		m.setNotice(safeText("undone: "+m.describe(msg.batch.Steps)), false)
	}
	m.caughtUp = time.Time{} // now, whatever the last one was
	cmd := m.catchUp(time.Now())
	// What came back is shown, as after a rename: searched for by name,
	// selected, no tag hiding it. Not what went to the trash: gone.
	for _, s := range msg.batch.Steps {
		back := s.Kind == mutation.OpRename || s.Kind == mutation.OpDelete
		if !back || !within(s.From, m.under()) {
			continue
		}
		m.tags = nil
		search := m.setQuery(filepath.Base(s.From))
		m.qpos = len(m.query)
		m.picked = s.From // after setQuery, which clears it; holds it while results move
		return tea.Batch(cmd, search)
	}
	return cmd
}

// describe names the change steps made, for a question or the status bar:
// the one step itself, or how many of each kind.
func (m *Model) describe(steps []mutation.Step) string {
	if len(steps) == 1 {
		s := steps[0]
		switch s.Kind {
		case mutation.OpDelete:
			return "trash " + filepath.Base(s.From)
		case mutation.OpRename:
			if filepath.Dir(s.From) == filepath.Dir(s.To) {
				return "rename " + filepath.Base(s.From) + " → " + filepath.Base(s.To)
			}
			return "move " + filepath.Base(s.From) + " → " + m.tildify(filepath.Dir(s.To))
		case mutation.OpCopy:
			return "copy " + filepath.Base(s.From) + " → " + m.tildify(s.To)
		case mutation.OpCreate:
			return "create " + filepath.Base(s.To)
		}
	}
	var n [4]int
	for _, s := range steps {
		if int(s.Kind) < len(n) {
			n[s.Kind]++
		}
	}
	text := fmt.Sprintf("%d changes:", len(steps))
	for _, kw := range []struct {
		kind mutation.Kind
		word string
	}{{mutation.OpRename, "renamed"}, {mutation.OpDelete, "trashed"}, {mutation.OpCopy, "copied"}, {mutation.OpCreate, "created"}} {
		if c := n[kw.kind]; c > 0 {
			text += fmt.Sprintf(" %d %s,", c, kw.word)
		}
	}
	return text[:len(text)-1]
}

// when says when t was, as a person would: the time today, the day and
// time before.
func when(t, now time.Time) string {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "today " + t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("Jan 2 15:04")
	}
	return t.Format("Jan 2 2006")
}
