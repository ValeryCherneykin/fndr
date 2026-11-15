package mutation

import (
	"context"
	"encoding/json"
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
	_, _ = RunIntent(context.Background(), in, trash, nil) //nolint:errcheck // a crash, or done: the disk is what is checked
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

// TestRecoverWhole: a plan not cut off recovers as the steps Run returned.
func TestRecoverWhole(t *testing.T) {
	dir, trash := crashFolder(t)
	in := Prepare(dir, crashPlan)
	steps, err := RunIntent(context.Background(), in, trash, nil)
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
