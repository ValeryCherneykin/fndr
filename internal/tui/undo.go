package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// batch is a change carried out: one write, one rename.
type batch struct {
	ID    string          `json:"id"`
	Time  time.Time       `json:"time"`
	Steps []mutation.Step `json:"steps"`
}

// record is one record of the journal: a batch done, or the ID of one
// undone. Never both.
type record struct {
	Batch  *batch `json:"batch,omitempty"`
	Undone string `json:"undone,omitempty"`
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
)

// journalPath is where batches are kept: beside the history.
func journalPath() (string, error) {
	hist, err := frecency.DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(hist), "journal"), nil
}

// record keeps the steps of a change carried out, for ctrl+z: in the
// journal, in a command (a disk write, synced), or in memory without one.
func (m *Model) record(steps []mutation.Step) tea.Cmd {
	if len(steps) == 0 {
		return nil
	}
	m.batchSeq++
	b := batch{
		// Unique across fndr running at once, in other panes.
		ID:    strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(os.Getpid()) + "-" + strconv.Itoa(m.batchSeq),
		Time:  time.Now(),
		Steps: steps,
	}
	if m.journal == "" {
		m.batches = append(m.batches, b)
		return nil
	}
	path := m.journal
	return func() tea.Msg { return recordedMsg{err: appendRecord(path, record{Batch: &b})} }
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
		case r.Batch != nil:
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
