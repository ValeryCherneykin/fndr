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

// snapshot returns what is under dir: a path below it for each entry, its
// bytes for a file, "/" for a folder.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p) //nolint:errcheck // p is below dir
		if d.IsDir() {
			out[rel] = "/"
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // the test's own files
		out[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// plant writes files (a trailing "/" is a folder) under dir.
func plant(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0o750); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// trashes are the two kinds of trash, for a test to run in both.
func trashes(t *testing.T) map[string]*Trash {
	return map[string]*Trash{
		"macOS": {files: filepath.Join(t.TempDir(), ".Trash")},
		"Linux": freedesktop(filepath.Join(t.TempDir(), "Trash")),
	}
}

// TestUndoAll: every kind of step, folders made on the way included, and
// Undo leaves the folder exactly as it was. What was made is in the trash,
// what was trashed is back, with no info file left behind for it.
func TestUndoAll(t *testing.T) {
	for name, trash := range trashes(t) {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			plant(t, dir, map[string]string{"keep.txt": "k", "old.txt": "o", "gone.txt": "g", "src/a.txt": "a"})
			before := snapshot(t, dir)
			plan := Plan{
				{Kind: OpDelete, From: "gone.txt"},
				{Kind: OpCopy, From: "src/", To: "out/lib/"}, // out made on the way
				{Kind: OpRename, From: "old.txt", To: "moved/deep/new.txt"},
				{Kind: OpCreate, To: "notes/x.md"},
				// Into the folder the rename made: undone first, newest
				// first, or the rename back would find moved/ not empty.
				{Kind: OpCreate, To: "moved/deep/z.txt"},
			}
			steps, err := Run(context.Background(), dir, plan, trash, nil)
			if err != nil || len(steps) != len(plan) {
				t.Fatalf("Run: %d steps, %v", len(steps), err)
			}
			if undone, err := Undo(steps, trash); err != nil || undone != len(steps) {
				t.Fatalf("Undo: %d, %v", undone, err)
			}
			if after := snapshot(t, dir); !maps.Equal(after, before) {
				t.Errorf("after undo %q, want %q", after, before)
			}
			inTrash := snapshot(t, trash.files)
			if _, ok := inTrash["x.md"]; !ok || inTrash["lib/a.txt"] != "a" {
				t.Errorf("trash %q: want the created x.md and the copy lib", inTrash)
			}
			if trash.info != "" {
				if infos := snapshot(t, trash.info); len(infos) != 3 {
					t.Errorf("info files %q: want the three put in the trash by Undo, gone.txt's taken out", infos)
				}
			}
		})
	}
}

// TestUndoChanged: a file moved on since is not the step's to take back:
// left where the user put it, said as ErrChanged.
func TestUndoChanged(t *testing.T) {
	dir := t.TempDir()
	plant(t, dir, map[string]string{"a.txt": "a"})
	steps, err := Run(context.Background(), dir, Plan{{Kind: OpRename, From: "a.txt", To: "b.txt"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "b.txt"), filepath.Join(dir, "c.txt")); err != nil {
		t.Fatal(err)
	}
	undone, err := Undo(steps, nil)
	if undone != 0 || err == nil || !strings.Contains(err.Error(), ErrChanged.Error()) {
		t.Errorf("Undo: %d, %v", undone, err)
	}
	if got := snapshot(t, dir); !maps.Equal(got, map[string]string{"c.txt": "a"}) {
		t.Errorf("%q: the user's c.txt must stay", got)
	}

	// Another file put at the step's place since is not the step's file.
	plant(t, dir, map[string]string{"b.txt": "someone else"})
	if undone, _ := Undo(steps, nil); undone != 0 {
		t.Error("undid onto a file that is not the step's")
	}
}

// TestUndoNameTaken: a file trashed, and a new one made at its name since:
// the new one stays, the old one stays in the trash.
func TestUndoNameTaken(t *testing.T) {
	trash := trashes(t)["macOS"]
	dir := t.TempDir()
	plant(t, dir, map[string]string{"x.txt": "old"})
	steps, err := Run(context.Background(), dir, Plan{{Kind: OpDelete, From: "x.txt"}}, trash, nil)
	if err != nil {
		t.Fatal(err)
	}
	plant(t, dir, map[string]string{"x.txt": "new"})
	if undone, err := Undo(steps, trash); undone != 0 || err == nil {
		t.Errorf("Undo: %d, %v", undone, err)
	}
	if got := snapshot(t, dir)["x.txt"]; got != "new" {
		t.Errorf("x.txt is %q", got)
	}
	if got := snapshot(t, trash.files)["x.txt"]; got != "old" {
		t.Errorf("trash x.txt is %q", got)
	}
}

// TestUndoFolderGone: a file trashed from a folder removed since comes
// back to it, made again.
func TestUndoFolderGone(t *testing.T) {
	trash := trashes(t)["macOS"]
	dir := t.TempDir()
	plant(t, dir, map[string]string{"d/x.txt": "x"})
	steps, err := Run(context.Background(), dir, Plan{{Kind: OpDelete, From: "d/x.txt"}}, trash, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "d")); err != nil {
		t.Fatal(err)
	}
	if undone, err := Undo(steps, trash); undone != 1 || err != nil {
		t.Fatalf("Undo: %d, %v", undone, err)
	}
	if got := snapshot(t, dir)["d/x.txt"]; got != "x" {
		t.Errorf("d/x.txt is %q", got)
	}
}

// TestUndoKeepsWhatIsWrittenIn: a created file written in since goes to
// the trash with what was written; its folder, with another file in it
// now, stays.
func TestUndoKeepsWhatIsWrittenIn(t *testing.T) {
	trash := trashes(t)["macOS"]
	dir := t.TempDir()
	steps, err := Run(context.Background(), dir, Plan{{Kind: OpCreate, To: "notes/x.md"}}, trash, nil)
	if err != nil {
		t.Fatal(err)
	}
	plant(t, dir, map[string]string{"notes/x.md": "my day", "notes/y.md": "y"})
	if undone, err := Undo(steps, trash); undone != 1 || err != nil {
		t.Fatalf("Undo: %d, %v", undone, err)
	}
	if got := snapshot(t, dir); !maps.Equal(got, map[string]string{"notes": "/", "notes/y.md": "y"}) {
		t.Errorf("%q", got)
	}
	if got := snapshot(t, trash.files)["x.md"]; got != "my day" {
		t.Errorf("trash x.md is %q: what was written is lost", got)
	}
}

// TestUndoHalfRun: a plan stopped by a failure returns the steps it took,
// and they undo to the folder as it was.
func TestUndoHalfRun(t *testing.T) {
	dir := t.TempDir()
	plant(t, dir, map[string]string{"a.txt": "a", "locked/": ""})
	if err := os.Chmod(filepath.Join(dir, "locked"), 0o500); err != nil { //nolint:gosec // read-only on purpose: the create in it fails
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "locked"), 0o700) }) //nolint:gosec // to let TempDir go
	before := snapshot(t, dir)
	trash := trashes(t)["macOS"]
	plan := Plan{{Kind: OpRename, From: "a.txt", To: "b.txt"}, {Kind: OpCreate, To: "locked/x"}}
	steps, err := Run(context.Background(), dir, plan, trash, nil)
	if err == nil || len(steps) != 1 {
		t.Fatalf("Run: %d steps, %v; want the rename only", len(steps), err)
	}
	if _, err := Undo(steps, trash); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, dir); !maps.Equal(after, before) {
		t.Errorf("%q, want %q", after, before)
	}
}

// TestUndoTwice: a second Undo of the same steps finds nothing of theirs
// and touches nothing.
func TestUndoTwice(t *testing.T) {
	trash := trashes(t)["macOS"]
	dir := t.TempDir()
	plant(t, dir, map[string]string{"a.txt": "a"})
	steps, err := Run(context.Background(), dir, Plan{{Kind: OpRename, From: "a.txt", To: "b.txt"}, {Kind: OpCreate, To: "n.txt"}}, trash, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Undo(steps, trash); err != nil {
		t.Fatal(err)
	}
	before, inTrash := snapshot(t, dir), snapshot(t, trash.files)
	if undone, _ := Undo(steps, trash); undone != 0 {
		t.Errorf("undid %d steps again", undone)
	}
	if !maps.Equal(snapshot(t, dir), before) || !maps.Equal(snapshot(t, trash.files), inTrash) {
		t.Error("a second Undo changed something")
	}
}

// TestStepJSON: a step reads back as written, its kind by name; a kind no
// fndr knows is an error, not a guess.
func TestStepJSON(t *testing.T) {
	for k := range kindNames {
		s := Step{Kind: k, From: "/a", To: "/b", Made: "/m", ID: FileID{Dev: 1, Ino: 2}}
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"`+kindNames[k]+`"`) {
			t.Errorf("%s: kind not by name", b)
		}
		var back Step
		if err := json.Unmarshal(b, &back); err != nil || back != s {
			t.Errorf("%s read back as %+v, %v", b, back, err)
		}
	}
	var s Step
	if err := json.Unmarshal([]byte(`{"kind":"explode","to":"/x"}`), &s); err == nil {
		t.Error("an unknown kind was read")
	}
	if _, err := json.Marshal(Step{Kind: Kind(99)}); err == nil {
		t.Error("an unknown kind was written")
	}
}

// TestUndoSwap: a swap goes back as it was made, in two phases: one by
// one, the first rename back finds its old name taken by the other.
func TestUndoSwap(t *testing.T) {
	dir := t.TempDir()
	plant(t, dir, map[string]string{"a.txt": "A", "b.txt": "B", "c.txt": "C"})
	before := snapshot(t, dir)
	swap := Plan{{Kind: OpRename, From: "a.txt", To: "b.txt"}, {Kind: OpRename, From: "b.txt", To: "c.txt"}, {Kind: OpRename, From: "c.txt", To: "a.txt"}}
	steps, err := Run(context.Background(), dir, swap, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if undone, err := Undo(steps, nil); undone != 3 || err != nil {
		t.Fatalf("Undo: %d, %v", undone, err)
	}
	if after := snapshot(t, dir); !maps.Equal(after, before) {
		t.Errorf("%q, want %q", after, before)
	}
}

// TestUndoRenameNameTaken: a rename taken back onto a name made since
// stays under the name the rename gave it, never a hidden temporary one.
func TestUndoRenameNameTaken(t *testing.T) {
	dir := t.TempDir()
	plant(t, dir, map[string]string{"a.txt": "A"})
	steps, err := Run(context.Background(), dir, Plan{{Kind: OpRename, From: "a.txt", To: "b.txt"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	plant(t, dir, map[string]string{"a.txt": "new"})
	if undone, err := Undo(steps, nil); undone != 0 || err == nil || !strings.Contains(err.Error(), "taken") {
		t.Errorf("Undo: %d, %v", undone, err)
	}
	if got := snapshot(t, dir); !maps.Equal(got, map[string]string{"a.txt": "new", "b.txt": "A"}) {
		t.Errorf("%q: b.txt must stay b.txt", got)
	}
}
