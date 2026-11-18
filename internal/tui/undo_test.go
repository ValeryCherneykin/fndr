package tui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ValeryCherneykin/fndr/internal/journal"
	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

func ctrlZ(m *Model) { key(m, 'z', tea.ModCtrl) }

// TestUndoRename: ctrl+z names what it would take back and when, changes
// nothing before y, and y puts the file back, found again and selected.
func TestUndoRename(t *testing.T) {
	m, root := renameModel(t)
	retype(m, "src/tools.go")
	from, to := filepath.Join(root, "src", "util.go"), filepath.Join(root, "src", "tools.go")

	ctrlZ(m)
	if m.undoing == nil || !strings.Contains(m.notice, "undo rename util.go → tools.go (today ") {
		t.Fatalf("offer: %q", m.notice)
	}
	if exists(from) || !exists(to) {
		t.Fatal("the disk changed before y")
	}
	m.caughtUp = time.Now() // a catch-up a moment ago must not keep this one from running
	typeText(m, "y")
	if !exists(from) || exists(to) {
		t.Fatalf("y did not put util.go back: notice %q", m.notice)
	}
	if len(m.results) == 0 || m.results[m.cursor].Entry.Path != from {
		t.Errorf("selected %q, want util.go, back and in sight", m.names())
	}
	if m.notice != "undone: rename util.go → tools.go" || m.noticeErr {
		t.Errorf("notice %q", m.notice)
	}
	if !m.indexed(from) || m.indexed(to) {
		t.Errorf("index: util.go %v, tools.go %v; want it caught up", m.indexed(from), m.indexed(to))
	}
}

// TestUndoTrash: what an OIL write put in the trash comes out of it.
func TestUndoTrash(t *testing.T) {
	m, root, _ := writeModel(t)
	typeText(m, "dd") // docs/
	command(m, "w")
	typeText(m, "y")
	key(m, tea.KeyEscape, 0) // back to search
	ctrlZ(m)
	if !strings.Contains(m.notice, "undo trash docs") {
		t.Fatalf("offer: %q", m.notice)
	}
	typeText(m, "y")
	if !exists(filepath.Join(root, "docs")) {
		t.Fatalf("docs is not back: notice %q", m.notice)
	}
}

// TestUndoAnswers: esc changes nothing and the offer comes again; with
// nothing done, there is nothing to offer; each ctrl+z takes the newest
// batch not taken back yet.
func TestUndoAnswers(t *testing.T) {
	m, root := browseModel(t, "")
	ctrlZ(m)
	if m.undoing != nil || m.notice != "nothing to undo" {
		t.Fatalf("nothing done: %q", m.notice)
	}

	typeText(m, "util")
	key(m, 'r', tea.ModCtrl)
	retype(m, "src/one.go")
	key(m, 'r', tea.ModCtrl) // one.go is the query and selected after the rename
	retype(m, "src/two.go")

	ctrlZ(m)
	key(m, tea.KeyEscape, 0)
	if m.undoing != nil || !exists(filepath.Join(root, "src", "two.go")) {
		t.Fatal("esc changed something")
	}
	ctrlZ(m)
	if !strings.Contains(m.notice, "one.go → two.go") {
		t.Fatalf("offered again: %q", m.notice)
	}
	typeText(m, "y")
	ctrlZ(m)
	if !strings.Contains(m.notice, "util.go → one.go") {
		t.Errorf("next: %q, want the rename before", m.notice)
	}
	typeText(m, "y")
	if !exists(filepath.Join(root, "src", "util.go")) {
		t.Error("util.go is not back")
	}
	ctrlZ(m)
	if m.notice != "nothing to undo" {
		t.Errorf("after both: %q", m.notice)
	}
}

// TestUndoNothingDone: a change refused before any step is not a batch:
// ctrl+z does not offer to take back nothing.
func TestUndoNothingDone(t *testing.T) {
	m, _ := renameModel(t)
	retype(m, "src/main.go") // taken
	if !m.noticeErr {
		t.Fatalf("the rename onto main.go was not refused: %q", m.notice)
	}
	key(m, tea.KeyEscape, 0)
	ctrlZ(m)
	if m.notice != "nothing to undo" {
		t.Errorf("offered %q", m.notice)
	}
}

// TestUndoOutlivesFndr: a change made by one fndr is undone by the next,
// through the journal on the disk, which then knows it is undone.
func TestUndoOutlivesFndr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "journal")
	first, root := renameModel(t)
	first.journal = path
	retype(first, "src/tools.go")
	if recs, err := journal.Read(path); err != nil || len(recs) != 2 {
		t.Fatalf("journal: %d records, %v; want the rename begun, then done", len(recs), err)
	}

	next, _ := browseModel(t, "")
	next.journal = path
	send(next, recoverJournal(path)()) // done, not cut off: nothing to recover
	if next.notice != "" {
		t.Fatalf("a change that ended was recovered: %q", next.notice)
	}
	ctrlZ(next)
	if !strings.Contains(next.notice, "util.go → tools.go") {
		t.Fatalf("the next fndr offers %q", next.notice)
	}
	typeText(next, "y")
	if !exists(filepath.Join(root, "src", "util.go")) {
		t.Fatalf("not undone: %q", next.notice)
	}
	if recs, _ := journal.Read(path); len(recs) != 4 {
		t.Errorf("journal: %d records, want begun, done, undo begun, undone", len(recs))
	}
	ctrlZ(first)
	if first.notice != "nothing to undo" {
		t.Errorf("the first fndr still offers %q", first.notice)
	}
}

// TestUndoPasteDropsOffer: a paste is typing: the offer is gone, and the
// y in it is a letter of the query.
func TestUndoPasteDropsOffer(t *testing.T) {
	m, root := renameModel(t)
	retype(m, "src/tools.go")
	ctrlZ(m)
	send(m, tea.PasteMsg{Content: "y"})
	if m.undoing != nil || exists(filepath.Join(root, "src", "util.go")) {
		t.Error("a pasted y undid")
	}
}

// TestLastBatch: the newest batch not undone; a record no fndr wrote (or
// a newer one did) is passed over, not the end of the rest.
func TestLastBatch(t *testing.T) {
	enc := func(r record) []byte { b, _ := json.Marshal(r); return b } //nolint:errcheck // test
	a := batch{ID: "a", Steps: []mutation.Step{{Kind: mutation.OpCreate, To: "/x"}}}
	b := batch{ID: "b", Steps: a.Steps}
	recs := [][]byte{enc(record{Batch: &a}), []byte("{not json"), enc(record{Batch: &b}), enc(record{Undone: "b"}), []byte(`{"future":1}`)}
	if got, ok := lastBatch(recs); !ok || got.ID != "a" {
		t.Errorf("got %q %v, want a", got.ID, ok)
	}
	if _, ok := lastBatch(append(recs, enc(record{Undone: "a"}))); ok {
		t.Error("both undone, and something offered")
	}
}

func TestWhen(t *testing.T) {
	now := time.Date(2026, 11, 15, 18, 0, 0, 0, time.Local)
	for at, want := range map[time.Time]string{
		now.Add(-time.Hour):                             "today 17:00",
		now.AddDate(0, 0, -1):                           "Nov 14 18:00",
		now.AddDate(-1, 0, 0):                           "Nov 15 2025",
		time.Date(2026, 11, 15, 0, 1, 0, 0, time.Local): "today 00:01",
	} {
		if got := when(at, now); got != want {
			t.Errorf("when(%v) = %q, want %q", at, got, want)
		}
	}
}

// deadPID returns the PID of a process that has ended: a change begun by
// it was cut off, not under way.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("no true:", err)
	}
	return cmd.Process.Pid
}

// crashedRename leaves on the disk what a ctrl+r of src/util.go to
// src/tools.go cut off between its two phases leaves: util.go under its
// hidden temporary name, and the change begun in the journal at path by a
// fndr that is gone. It returns the hidden name.
func crashedRename(t *testing.T, root, path string, pid int) string {
	t.Helper()
	dir := filepath.Join(root, "src")
	in := mutation.Prepare(dir, mutation.Plan{{Kind: mutation.OpRename, From: "util.go", To: "tools.go"}})
	in.PID = pid
	in.Ops[0].Temp = filepath.Join(dir, ".fndr-rename-"+strconv.Itoa(pid)+"-0")
	if err := os.Rename(filepath.Join(dir, "util.go"), in.Ops[0].Temp); err != nil {
		t.Fatal(err)
	}
	if err := (journaled{path: path, id: "cut"}).begin(in); err != nil {
		t.Fatal(err)
	}
	return in.Ops[0].Temp
}

// TestRecoverCutOff: a change cut off by a crash is put right by the next
// fndr to start: nothing left under a hidden name, said in the status
// bar, and ctrl+z takes it back like any other.
func TestRecoverCutOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	m, root := browseModel(t, "")
	m.journal = path
	hidden := crashedRename(t, root, path, deadPID(t))

	send(m, recoverJournal(path)())
	if exists(hidden) || !exists(filepath.Join(root, "src", "tools.go")) {
		t.Fatalf("not put right: hidden there %v", exists(hidden))
	}
	if !strings.Contains(m.notice, "cut off last time: 1 of 1 done, 1 put back from a hidden name") {
		t.Errorf("notice %q", m.notice)
	}
	ctrlZ(m)
	if !strings.Contains(m.notice, "util.go → tools.go") {
		t.Fatalf("ctrl+z offers %q", m.notice)
	}
	typeText(m, "y")
	if !exists(filepath.Join(root, "src", "util.go")) {
		t.Errorf("not undone: %q", m.notice)
	}

	// Once put right, the next start finds nothing to do.
	m.setNotice("", false)
	send(m, recoverJournal(path)())
	if m.notice != "" {
		t.Errorf("recovered twice: %q", m.notice)
	}
}

func TestRecoverUsesPreparedWrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replace bool
	}{
		{name: "published"},
		{name: "replaced", replace: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", "")
			m, root := browseModel(t, "")
			m.journal = filepath.Join(t.TempDir(), "journal")
			j := journaled{path: m.journal, id: "cut"}
			in := mutation.Prepare(root, mutation.Plan{
				{Kind: mutation.OpCopy, From: "README.md", To: "copy.md"},
				{Kind: mutation.OpCreate, To: "new.md"},
			})
			in.PID = deadPID(t)
			if err := j.begin(in); err != nil {
				t.Fatal(err)
			}
			if _, err := mutation.RunIntent(t.Context(), in, nil, nil, j.prepare); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"copy.md", "new.md"} {
				if tc.replace {
					p := filepath.Join(root, name)
					if err := os.Rename(p, p+".ours"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(p, []byte("external"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}

			send(m, recoverJournal(m.journal)())
			ctrlZ(m)
			if tc.replace {
				if m.notice != "nothing to undo" {
					t.Fatalf("foreign files offered for undo: %q", m.notice)
				}
			} else {
				if !strings.Contains(m.notice, "1 copied, 1 created") {
					t.Fatalf("missing recovered undo: %q", m.notice)
				}
				typeText(m, "y")
			}
			for _, name := range []string{"copy.md", "new.md"} {
				if exists(filepath.Join(root, name)) != tc.replace {
					t.Errorf("unexpected presence of %q after undo: %q", name, m.notice)
				}
			}
		})
	}
}

// TestRecoverLeavesLiveFndr: a change begun by a fndr still running, in
// another pane, is under way, not cut off: left alone.
func TestRecoverLeavesLiveFndr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	m, root := browseModel(t, "")
	m.journal = path
	hidden := crashedRename(t, root, path, os.Getpid())
	send(m, recoverJournal(path)())
	if !exists(hidden) || m.notice != "" {
		t.Errorf("a live fndr's change was touched: hidden there %v, notice %q", exists(hidden), m.notice)
	}
}

// TestRefusedIsNotCutOff: a write refused before its first step was begun
// in the journal; it is ended there too, or the next start would take
// the .env that was already there for one the write made, and ctrl+z
// offer it to the trash.
func TestRefusedIsNotCutOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	m, root, _ := writeModel(t)
	m.journal = path
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	typeText(m, "o.env") // hidden, out of the buffer: only the disk says it is taken
	key(m, tea.KeyEscape, 0)
	command(m, "w")
	typeText(m, "y")
	if !m.noticeErr {
		t.Fatalf("the create over .env was not refused: %q", m.notice)
	}
	if recs, _ := journal.Read(path); len(recs) != 2 {
		t.Fatalf("journal: %d records, want it begun and ended", len(recs))
	}
	m.setNotice("", false)
	send(m, recoverJournal(path)())
	if m.notice != "" {
		t.Errorf("recovered: %q", m.notice)
	}
}

// TestStartRecovers: fndr starting with a change cut off puts it right
// before anything else is asked of it.
func TestStartRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	m, root := browseModel(t, "")
	m.journal = path
	hidden := crashedRename(t, root, path, deadPID(t))
	for _, msg := range runCmd(m.Init()) {
		send(m, msg)
	}
	if exists(hidden) {
		t.Error("start did not recover")
	}
}

// TestCutOff: a change begun and ended is not cut off; one only begun is.
func TestCutOff(t *testing.T) {
	enc := func(r record) []byte { b, _ := json.Marshal(r); return b } //nolint:errcheck // test
	recs := [][]byte{
		enc(record{Begin: &begun{ID: "a"}}),
		enc(record{Begin: &begun{ID: "b"}}),
		enc(record{Batch: &batch{ID: "a"}}),
		[]byte("{torn"),
	}
	if got := cutOff(recs); len(got) != 1 || got[0].ID != "b" {
		t.Errorf("cut off: %+v, want b alone", got)
	}
}

// TestRecoverReadsAgain: a copy cut off leaves a hidden half copy, in the
// listing read at start; recovery takes it away, and the listing with it,
// though there is nothing to say: the copy never was.
func TestRecoverReadsAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	m, root := browseModel(t, "")
	m.journal = path
	pid := deadPID(t)
	half := filepath.Join(root, ".fndr-copy-"+strconv.Itoa(pid)+"-1")
	if err := os.WriteFile(half, []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	send(m, m.load(root)())
	listed := func() bool {
		for _, r := range m.listing {
			if r.Entry.Path == half {
				return true
			}
		}
		return false
	}
	if !listed() {
		t.Fatal("the half copy is not in the listing to begin with")
	}
	in := mutation.Prepare(root, mutation.Plan{{Kind: mutation.OpCopy, From: "README.md", To: "README copy.md"}})
	in.PID = pid
	in.Ops[0].Temp = ""
	if err := (journaled{path: path, id: "cut"}).begin(in); err != nil {
		t.Fatal(err)
	}
	send(m, recoverJournal(path)())
	if exists(half) || listed() || m.notice != "" {
		t.Errorf("half there %v, listed %v, notice %q", exists(half), listed(), m.notice)
	}
}

// TestRecoverNothingCostsNothing: with nothing cut off, the start does not
// read the disk again for it: a catch-up is some 50-100 ms of a home.
func TestRecoverNothingCostsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	m, _ := renameModel(t)
	m.journal = path
	retype(m, "src/tools.go") // begun and ended: nothing cut off
	m.caughtUp = time.Time{}
	send(m, recoverJournal(path)())
	if !m.caughtUp.IsZero() {
		t.Error("caught up with nothing cut off")
	}
}

// crashedUndo leaves what an undo of the last batch in the journal at
// path, by a fndr gone (pid), cut off between its two phases leaves: the
// renamed file under the undo's hidden name, the undo begun.
func crashedUndo(t *testing.T, path string, pid int) (hiddenName string) {
	t.Helper()
	recs, err := journal.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := lastBatch(recs)
	if !ok || len(b.Steps) != 1 {
		t.Fatalf("no batch of one step to undo: %+v", b)
	}
	s := b.Steps[0]
	hiddenName = filepath.Join(filepath.Dir(s.To), ".fndr-undo-"+strconv.Itoa(pid)+"-0")
	if err := os.Rename(s.To, hiddenName); err != nil {
		t.Fatal(err)
	}
	if err := appendRecord(path, record{Undoing: &undoing{ID: b.ID, PID: pid}}); err != nil {
		t.Fatal(err)
	}
	return hiddenName
}

// TestUndoCutOff: an undo cut off is put right at the next start, the file
// at its old place, said so; ctrl+z then finishes the undo with no error,
// and after that there is nothing to undo, nor to recover.
func TestUndoCutOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	m, root := renameModel(t)
	m.journal = path
	retype(m, "src/tools.go")
	hiddenName := crashedUndo(t, path, deadPID(t))

	send(m, recoverJournal(path)())
	if exists(hiddenName) || !exists(filepath.Join(root, "src", "util.go")) {
		t.Fatalf("not put right: hidden there %v", exists(hiddenName))
	}
	if m.notice != "an undo was cut off last time, 1 put back from a hidden name — ctrl+z finishes the undo" {
		t.Errorf("notice %q", m.notice)
	}
	m.setNotice("", false)
	send(m, recoverJournal(path)()) // put right once is enough: the next start says nothing
	if m.notice != "" {
		t.Errorf("put right twice: %q", m.notice)
	}
	ctrlZ(m)
	if !strings.Contains(m.notice, "util.go → tools.go") {
		t.Fatalf("ctrl+z offers %q", m.notice)
	}
	typeText(m, "y")
	if m.noticeErr || !exists(filepath.Join(root, "src", "util.go")) {
		t.Fatalf("finishing: %q", m.notice)
	}
	ctrlZ(m)
	if m.notice != "nothing to undo" {
		t.Errorf("after: %q", m.notice)
	}
	m.setNotice("", false)
	send(m, recoverJournal(path)())
	if m.notice != "" {
		t.Errorf("recovered again: %q", m.notice)
	}
}

// TestUndoUnderWayLeftAlone: an undo by a fndr still running is under way:
// its hidden name is not touched.
func TestUndoUnderWayLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	m, _ := renameModel(t)
	m.journal = path
	retype(m, "src/tools.go")
	hiddenName := crashedUndo(t, path, os.Getpid())
	m.setNotice("", false)
	send(m, recoverJournal(path)())
	if !exists(hiddenName) || m.notice != "" {
		t.Errorf("a live undo was touched: %v, %q", exists(hiddenName), m.notice)
	}
}

// TestUndosCutOff: an undo is over once undone or put right; a second
// undo of the same batch begun after is a new one.
func TestUndosCutOff(t *testing.T) {
	enc := func(r record) []byte { b, _ := json.Marshal(r); return b } //nolint:errcheck // test
	steps := []mutation.Step{{Kind: mutation.OpRename, From: "/a", To: "/b"}}
	recs := [][]byte{
		enc(record{Batch: &batch{ID: "x", Steps: steps}}),
		enc(record{Undoing: &undoing{ID: "x", PID: 1}}),
		enc(record{Recovered: "x"}),
		enc(record{Batch: &batch{ID: "y", Steps: steps}}),
		enc(record{Undoing: &undoing{ID: "y", PID: 2}}),
		enc(record{Undone: "y"}),
		enc(record{Undoing: &undoing{ID: "x", PID: 3}}),
	}
	if got := undosCutOff(recs[:len(recs)-1]); len(got) != 0 {
		t.Errorf("all over, and %+v", got)
	}
	if got := undosCutOff(recs); len(got) != 1 || got[0].ID != "x" || got[0].PID != 3 || len(got[0].steps) != 1 {
		t.Errorf("x begun again: %+v", got)
	}
}

// TestUndoCutOffEarly: an undo cut off before it hid anything is still
// said: part of the batch may be back, and ctrl+z finishes it.
func TestUndoCutOffEarly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	m, _ := renameModel(t)
	m.journal = path
	retype(m, "src/tools.go")
	recs, _ := journal.Read(path)
	b, _ := lastBatch(recs)
	if err := appendRecord(path, record{Undoing: &undoing{ID: b.ID, PID: deadPID(t)}}); err != nil {
		t.Fatal(err)
	}
	send(m, recoverJournal(path)())
	if m.notice != "an undo was cut off last time — ctrl+z finishes the undo" {
		t.Errorf("notice %q", m.notice)
	}
}
