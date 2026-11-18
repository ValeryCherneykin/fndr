package mutation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Undo takes back what Run did, step by step, newest first, as a saga does
// its compensations: a rename goes back, the trash gives back, a create or
// a copy goes to the trash. It is safe however long after Run it comes,
// and whatever the user did in between, by four rules:
//
//   - A step is undone only on the very file it made: the one at its place
//     must be the same file (device and inode, which a rename and the trash
//     keep), or the step is skipped as ErrChanged. Never the wrong file.
//   - Nothing is deleted: what a create or a copy made goes to the trash,
//     so a file written in since is not lost either.
//   - Nothing is replaced: a name taken since is left to what took it.
//   - Folders a step made on the way go with it only if empty.
//
// A step skipped does not stop the others: each is a change of its own,
// and every one that can go back does. A step already back (its file at
// its old place, or what it made gone) counts as undone: Undo done again,
// after one cut off by a crash (see RecoverUndo), finishes the rest and
// says nothing of what was finished before.

// ErrChanged is a step whose file is not what the step left: moved,
// replaced or gone since.
var ErrChanged = errors.New("changed since")

// Step is a change Run made to the disk, with what Undo needs to take it
// back. Paths are absolute: a step is undone from anywhere, any time.
type Step struct {
	Kind Kind `json:"kind"`
	// From is where the entry was: the name before a rename, the place
	// before the trash, the source of a copy. "" for a create.
	From string `json:"from,omitempty"`
	// To is where the step left it: the new name, the trash, the copy,
	// what was created.
	To string `json:"to"`
	// Made is the top folder Run made on the way to To ("a" for a/b/c.txt
	// with no a before), or "".
	Made string `json:"made,omitempty"`
	// ID is To's file right after the step.
	ID    FileID `json:"id"`
	Birth int64  `json:"birth,omitempty"`
}

// FileID tells one file from another for as long as it exists, wherever it
// is moved on its disk.
type FileID struct {
	Dev uint64 `json:"dev"`
	Ino uint64 `json:"ino"`
}

// idOf returns the identity of the file at path, not following a link.
func idOf(path string) (FileID, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return FileID{}, err
	}
	return fileID(info)
}

func fileID(info os.FileInfo) (FileID, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FileID{}, errors.New("no inode here")
	}
	return FileID{Dev: uint64(st.Dev), Ino: st.Ino}, nil //nolint:gosec,unconvert // Dev is int32 on macOS, uint64 on Linux; never negative
}

// took is the step of kind from from to to, with To's identity as it is
// now, just after. Not knowing it (the file gone at once) leaves a zero
// ID, which nothing matches: that step is never undone, never the wrong
// file.
func took(kind Kind, from, to, made string) Step {
	if kind == OpCreate {
		proof, _ := stamp(to) //nolint:errcheck // an unknown identity cannot be undone
		return Step{Kind: kind, From: from, To: to, Made: made, ID: proof.ID, Birth: proof.Birth}
	}
	id, _ := idOf(to) //nolint:errcheck // see above
	return Step{Kind: kind, From: from, To: to, Made: made, ID: id}
}

// madeIn is missingTop of path, done in dir, the folder a plan runs in:
// most of a plan's names are right in it, there by definition, and a
// thousand renames are spared a thousand lstats of it.
func madeIn(dir, path string) string {
	if filepath.Dir(path) == dir {
		return ""
	}
	return missingTop(path)
}

// missingTop returns the topmost of path's folders that are not there yet:
// what making path will make on the way. "" if its folder is there.
func missingTop(path string) string {
	top := ""
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		if _, err := os.Lstat(p); err == nil {
			return top
		}
		top = p
		if filepath.Dir(p) == p {
			return top
		}
	}
}

// Undo takes steps back, the last first (see above), and returns how many
// went back; err, one line for a status bar, names each one that did not.
// trash takes what creates and copies made; with none, they stay.
func Undo(steps []Step, trash *Trash) (undone int, err error) {
	var failed []string
	pid := os.Getpid()
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Kind == OpRename {
			// Renames go back together, in two phases, as Run made them:
			// one by one, a swap's first one back finds its old name
			// taken by the other.
			j := i
			for j > 0 && steps[j-1].Kind == OpRename {
				j--
			}
			n, f := undoRenames(steps[j:i+1], j, pid)
			undone, failed, i = undone+n, append(failed, f...), j
			continue
		}
		s := steps[i]
		if err := undoStep(s, trash); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", filepath.Base(s.To), err))
			continue
		}
		undone++
	}
	if len(failed) > 0 {
		return undone, errors.New(strings.Join(failed, ", "))
	}
	return undone, nil
}

// undoRenames takes renames back in two phases: each to a temporary name
// beside it, then each from there to its old name; one whose old name is
// taken goes back to where it was. So a swap, or a chain, goes back
// whole, whatever the order.
//
// first is the index of steps[0] among all the batch's: a temporary name
// is the step's own, .fndr-undo-<pid>-<index> beside it, so RecoverUndo,
// knowing the steps and the pid, knows where to look.
func undoRenames(steps []Step, first, pid int) (undone int, failed []string) {
	temps := make([]string, len(steps))
	for i, s := range steps {
		fail := func(err error) { failed = append(failed, fmt.Sprintf("%s: %v", filepath.Base(s.To), err)) }
		if is(s.From, s.ID) {
			removeEmpty(filepath.Dir(s.To), s.Made) // the rest of it, if cut off before
			undone++                                // back already, by an undo cut off
			continue
		}
		if !is(s.To, s.ID) {
			fail(ErrChanged)
			continue
		}
		temp := undoTemp(s, pid, first+i)
		if err := noReplace(s.To, temp); err != nil {
			fail(err)
			continue
		}
		crashPoint()
		temps[i] = temp
	}
	for i, s := range steps {
		if temps[i] == "" {
			continue
		}
		err := os.MkdirAll(filepath.Dir(s.From), 0o755) //nolint:gosec // the user's own folder: their umask decides
		if err == nil {
			err = noReplace(temps[i], s.From)
		}
		if err != nil {
			// Back where the step left it, under its own name, not a
			// hidden one.
			_ = noReplace(temps[i], s.To) //nolint:errcheck // its own name, freed by phase one
			failed = append(failed, fmt.Sprintf("%s: %s is taken", filepath.Base(s.To), filepath.Base(s.From)))
			continue
		}
		crashPoint()
		removeEmpty(filepath.Dir(s.To), s.Made)
		undone++
	}
	return undone, failed
}

// undoTemp is the temporary name undo by process pid gives the i-th step
// of a batch, s, between its two phases.
func undoTemp(s Step, pid, i int) string {
	return filepath.Join(filepath.Dir(s.To), ".fndr-undo-"+strconv.Itoa(pid)+"-"+strconv.Itoa(i))
}

// RecoverUndo puts right an Undo of steps by process pid that a crash cut
// off: a rename stuck under its temporary name goes on to its old name,
// as the undo meant, or back to its new one if that is taken; fixed is how
// many. The rest is Undo's again: done once more, it finishes what is left
// (see Undo).
func RecoverUndo(steps []Step, pid int) (fixed int, err error) {
	var failed []string
	for i, s := range steps {
		temp := undoTemp(s, pid, i)
		if s.Kind != OpRename || !is(temp, s.ID) {
			continue
		}
		mkErr := os.MkdirAll(filepath.Dir(s.From), 0o755) //nolint:gosec // the user's own folder
		switch {
		case mkErr == nil && noReplace(temp, s.From) == nil:
			removeEmpty(filepath.Dir(s.To), s.Made)
			fixed++
		case noReplace(temp, s.To) == nil:
			fixed++
		default:
			failed = append(failed, fmt.Sprintf("%q is left as %q", s.To, filepath.Base(temp)))
		}
	}
	if len(failed) > 0 {
		return fixed, errors.New(strings.Join(failed, ", "))
	}
	return fixed, nil
}

// undoStep takes one step back.
func undoStep(s Step, trash *Trash) error {
	if back(s) {
		// By an undo cut off before, maybe right after the step and
		// before its folders: they go now.
		removeEmpty(filepath.Dir(s.To), s.Made)
		return nil
	}
	if !matchesStep(s) {
		return ErrChanged
	}
	switch s.Kind {
	case OpDelete, OpRename:
		if err := os.MkdirAll(filepath.Dir(s.From), 0o755); err != nil { //nolint:gosec // the user's own folder: their umask decides
			return err
		}
		if err := noReplace(s.To, s.From); err != nil {
			return fmt.Errorf("%s is taken: %w", filepath.Base(s.From), err)
		}
		if s.Kind == OpDelete && trash != nil && trash.info != "" {
			// Out of the trash, its freedesktop info file goes too, or the
			// file manager would list a ghost.
			_ = os.Remove(filepath.Join(trash.info, filepath.Base(s.To)+".trashinfo")) //nolint:errcheck // a ghost at worst
		}
	case OpCreate, OpCopy:
		if trash == nil {
			return errors.New("no trash")
		}
		if _, err := trash.Put(s.To); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown step %v", s.Kind)
	}
	crashPoint()
	removeEmpty(filepath.Dir(s.To), s.Made)
	return nil
}

func matchesStep(s Step) bool {
	if s.Birth == 0 {
		return is(s.To, s.ID)
	}
	got, err := stamp(s.To)
	return err == nil && got == (FileStamp{ID: s.ID, Birth: s.Birth})
}

// back reports whether step s is undone already: its file at its old
// place, or, for what it made, nothing of it left where it put it.
func back(s Step) bool {
	switch s.Kind {
	case OpDelete, OpRename:
		return is(s.From, s.ID)
	case OpCreate, OpCopy:
		_, err := os.Lstat(s.To)
		return errors.Is(err, os.ErrNotExist)
	}
	return false
}

// removeEmpty removes dir and the folders above it up to made, as long as
// each is empty: what a step made on the way, and nothing put in it since.
func removeEmpty(dir, made string) {
	if made == "" {
		return
	}
	for p := dir; p == made || strings.HasPrefix(p, made+string(filepath.Separator)); p = filepath.Dir(p) {
		if os.Remove(p) != nil { // not empty, or not there: stop
			return
		}
	}
}

// kindNames are a Kind's names in a journal: words, not numbers, so a
// journal written by one fndr is read right by the next, whatever order
// the constants come in then.
var kindNames = map[Kind]string{OpDelete: "delete", OpRename: "rename", OpCreate: "create", OpCopy: "copy"}

// MarshalText writes k by its name.
func (k Kind) MarshalText() ([]byte, error) {
	if name, ok := kindNames[k]; ok {
		return []byte(name), nil
	}
	return nil, fmt.Errorf("unknown kind %d", int(k))
}

// UnmarshalText reads a Kind by its name.
func (k *Kind) UnmarshalText(b []byte) error {
	for kind, name := range kindNames {
		if name == string(b) {
			*k = kind
			return nil
		}
	}
	return fmt.Errorf("unknown kind %q", b)
}
