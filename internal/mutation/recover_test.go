package mutation

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// crashPlan has every kind of step, and the hard cases: a swap, a move
// into a folder made on the way, a copy into one.
var crashPlan = Plan{
	{Kind: OpDelete, From: "gone.txt"},
	{Kind: OpCopy, From: "src/", To: "out/lib/"},
	{Kind: OpRename, From: "a.txt", To: "b.txt"},
	{Kind: OpRename, From: "b.txt", To: "a.txt"},
	{Kind: OpRename, From: "c.txt", To: "moved/c.txt"},
	{Kind: OpCreate, To: "notes/x.md"},
}

func crashFolder(t *testing.T) (dir string, trash *Trash) {
	t.Helper()
	dir = t.TempDir()
	plant(t, dir, map[string]string{"gone.txt": "g", "src/s.txt": "s", "a.txt": "A", "b.txt": "B", "c.txt": "C"})
	return dir, &Trash{files: filepath.Join(t.TempDir(), ".Trash")}
}

// runUntil runs in, stopped as by a crash after the n-th change to the
// disk; n < 0 never stops it. It returns how many changes there were.
func runUntil(in Intent, trash *Trash, n int) (changes int) {
	type crash struct{}
	defer func() {
		if r := recover(); r != nil && r != (crash{}) {
			panic(r)
		}
	}()
	crashPoint = func() {
		changes++
		if changes == n {
			panic(crash{})
		}
	}
	defer func() { crashPoint = func() {} }()
	_, _ = RunIntent(context.Background(), in, trash, nil, func(files []PreparedFile) error { //nolint:errcheck // a crash, or done: the disk is what is checked
		for _, file := range files {
			in.Ops[file.Op].Proof = file.Proof
		}
		return nil
	})
	return changes
}

// hidden returns the names fndr hides its work under, left in dir.
func hidden(t *testing.T, dir string) []string {
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

// TestRecoverAnywhere: cut off after any change it makes, a plan is made
// sense of by Recover: nothing is left under a hidden name, a second
// Recover changes nothing, and the steps it returns undo to the folder as
// it was before, byte for byte: nothing done is lost, nothing is made up.
// The intent goes through JSON, as it does through the journal.
func TestRecoverAnywhere(t *testing.T) {
	dir, trash := crashFolder(t)
	total := runUntil(Prepare(dir, crashPlan), trash, -1)
	if total < len(crashPlan) {
		t.Fatalf("%d changes for %d ops", total, len(crashPlan))
	}
	for n := 0; n <= total; n++ {
		dir, trash := crashFolder(t)
		before := snapshot(t, dir)
		data, err := json.Marshal(Prepare(dir, crashPlan))
		if err != nil {
			t.Fatal(err)
		}
		var in Intent
		if err := json.Unmarshal(data, &in); err != nil {
			t.Fatal(err)
		}
		runUntil(in, trash, n)

		steps, _, err := Recover(in, trash)
		if err != nil {
			t.Fatalf("crash after %d: Recover: %v", n, err)
		}
		if left := hidden(t, dir); len(left) > 0 {
			t.Fatalf("crash after %d: left hidden: %q", n, left)
		}
		again, fixed, err := Recover(in, trash)
		if err != nil || fixed != 0 || len(again) != len(steps) {
			t.Fatalf("crash after %d: again: %d steps (was %d), %d fixed, %v", n, len(again), len(steps), fixed, err)
		}
		if _, err := Undo(steps, trash); err != nil {
			t.Fatalf("crash after %d: Undo: %v", n, err)
		}
		if after := snapshot(t, dir); !maps.Equal(after, before) {
			t.Fatalf("crash after %d: undone to %q, want %q", n, after, before)
		}
	}
}

func TestRecoverLeavesExistingDestinations(t *testing.T) {
	dir := tree(t, "source", "existing-copy", "existing-file")
	p := Plan{
		{Kind: OpCopy, From: "source", To: "existing-copy"},
		{Kind: OpCreate, To: "existing-file"},
	}
	steps, _, err := Recover(Prepare(dir, p), nil)
	if err != nil || len(steps) != 0 {
		t.Fatalf("Recover = %v, %v, want no applied steps", steps, err)
	}
}

func TestRecoverLeavesUnverifiedDestinations(t *testing.T) {
	dir := tree(t, "source")
	in := Prepare(dir, Plan{{Kind: OpCopy, From: "source", To: "copy"}})
	plant(t, dir, map[string]string{"copy": "external"})
	for range 2 {
		steps, _, err := Recover(in, nil)
		if err != nil || len(steps) != 0 || content(t, filepath.Join(dir, "copy")) != "external" {
			t.Fatalf("Recover = %v, %v; foreign file must stay untouched", steps, err)
		}
	}
}

func TestWriteDoesNotPublishWithoutProof(t *testing.T) {
	dir := tree(t, "source")
	refused := errors.New("journal unavailable")
	for _, op := range []Op{{Kind: OpCopy, From: "source", To: "copy"}, {Kind: OpCreate, To: "new/"}} {
		in := Prepare(dir, Plan{op})
		steps, err := RunIntent(t.Context(), in, nil, nil, func([]PreparedFile) error { return refused })
		if !errors.Is(err, refused) || len(steps) != 0 {
			t.Fatalf("RunIntent = %v, %v, want journal error", steps, err)
		}
		if _, err := os.Lstat(Target(dir, op.To)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("published %q without proof: %v", op.To, err)
		}
		if left := hidden(t, dir); len(left) > 0 {
			t.Fatalf("staging files left behind: %q", left)
		}
	}
}

func TestSiblingWritesShareProofRecord(t *testing.T) {
	dir := tree(t, "source")
	in := Prepare(dir, Plan{
		{Kind: OpCopy, From: "source", To: "first"},
		{Kind: OpCopy, From: "source", To: "second"},
	})
	saves := 0
	steps, err := RunIntent(t.Context(), in, nil, nil, func(files []PreparedFile) error {
		saves++
		if len(files) != 2 {
			t.Fatalf("prepared %d files, want 2", len(files))
		}
		for _, f := range files {
			to := Target(dir, in.Plan[f.Op].To)
			if _, err := os.Lstat(to); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%q was published before the journal: %v", to, err)
			}
			if !is(in.Ops[f.Op].Temp, f.Proof.ID) {
				t.Fatalf("proof does not identify the staged copy at operation %d", f.Op)
			}
		}
		return nil
	})
	if err != nil || len(steps) != 2 || saves != 1 {
		t.Fatalf("RunIntent = %v, %v, saved %d times", steps, err, saves)
	}
}

func TestCopyLeavesDestinationCreatedAfterProof(t *testing.T) {
	dir := tree(t, "source")
	in := Prepare(dir, Plan{{Kind: OpCopy, From: "source", To: "copy"}})
	steps, err := RunIntent(t.Context(), in, nil, nil, func(files []PreparedFile) error {
		in.Ops[0].Proof = files[0].Proof
		return os.WriteFile(filepath.Join(dir, "copy"), []byte("external"), 0o600)
	})
	if !errors.Is(err, ErrExists) || len(steps) != 0 {
		t.Fatalf("RunIntent = %v, %v, want occupied destination", steps, err)
	}
	got, _, err := Recover(in, nil)
	if err != nil || len(got) != 0 || content(t, filepath.Join(dir, "copy")) != "external" {
		t.Fatalf("Recover = %v, %v, want untouched external file", got, err)
	}
}

// TestRecoverWhole: a plan not cut off recovers as the steps Run returned.
func TestRecoverWhole(t *testing.T) {
	dir, trash := crashFolder(t)
	in := Prepare(dir, crashPlan)
	steps, err := RunIntent(context.Background(), in, trash, nil, func(files []PreparedFile) error {
		for _, file := range files {
			in.Ops[file.Op].Proof = file.Proof
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, fixed, err := Recover(in, trash)
	if err != nil || fixed != 0 || len(got) != len(steps) {
		t.Fatalf("%d steps, %d fixed, %v; Run took %d", len(got), fixed, err, len(steps))
	}
	for i := range steps {
		if got[i] != steps[i] {
			t.Errorf("step %d: %+v, Run took %+v", i, got[i], steps[i])
		}
	}
}

// undoUntil runs Undo of steps, stopped as by a crash after the n-th
// change to the disk; n < 0 never stops it. It returns how many there were.
func undoUntil(steps []Step, trash *Trash, n int) (changes int) {
	type crash struct{}
	defer func() {
		if r := recover(); r != nil && r != (crash{}) {
			panic(r)
		}
	}()
	crashPoint = func() {
		changes++
		if changes == n {
			panic(crash{})
		}
	}
	defer func() { crashPoint = func() {} }()
	_, _ = Undo(steps, trash) //nolint:errcheck // the disk is what is checked
	return changes
}

// TestUndoRecoverAnywhere: an undo cut off after any change it makes is
// put right by RecoverUndo, nothing left under a hidden name, and Undo
// done again finishes it, with no error: the folder is as it was before
// the plan, byte for byte.
func TestUndoRecoverAnywhere(t *testing.T) {
	setup := func() (string, *Trash, map[string]string, []Step) {
		dir, trash := crashFolder(t)
		before := snapshot(t, dir)
		steps, err := Run(context.Background(), dir, crashPlan, trash, nil)
		if err != nil {
			t.Fatal(err)
		}
		return dir, trash, before, steps
	}
	_, trash, _, steps := setup()
	total := undoUntil(steps, trash, -1)
	if total < len(steps) {
		t.Fatalf("%d changes for %d steps", total, len(steps))
	}
	for n := 1; n <= total; n++ {
		dir, trash, before, steps := setup()
		undoUntil(steps, trash, n)
		if _, err := RecoverUndo(steps, os.Getpid()); err != nil {
			t.Fatalf("cut after %d: RecoverUndo: %v", n, err)
		}
		if left := hidden(t, dir); len(left) > 0 {
			t.Fatalf("cut after %d: left hidden: %q", n, left)
		}
		if undone, err := Undo(steps, trash); err != nil || undone != len(steps) {
			t.Fatalf("cut after %d: Undo again: %d of %d, %v", n, undone, len(steps), err)
		}
		if after := snapshot(t, dir); !maps.Equal(after, before) {
			t.Fatalf("cut after %d: %q, want %q", n, after, before)
		}
	}
}
