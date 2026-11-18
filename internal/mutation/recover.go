package mutation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// A plan can be cut off anywhere: kill -9, a closed window, a dead
// battery. What was done is then on the disk and nowhere else, and some of
// it under names the user never chose: a rename between its two phases
// leaves the entry under a hidden .fndr-rename-*, a copy halfway leaves a
// hidden .fndr-copy-*. So a plan is first written down as an Intent, every
// name it will use in it, and the identity of every entry it will move;
// after a crash Recover reads the disk against it, as a database replays
// its write-ahead log:
//
//   - what was done becomes steps again, for ctrl+z;
//   - a rename stuck under its temporary name is finished: moved on to its
//     new name if that is free, else back to its old one. Forward first,
//     as the user asked for it, and because back may be taken: in a swap
//     cut off halfway, the old name holds the other half;
//   - a copy cut off, under its temporary name, is fndr's own and goes;
//   - a folder made on the way for what was not done goes, if empty;
//   - nothing else is touched.
//
// Recover can run twice, or be cut off itself: each check is made against
// the disk as it is, so done again it finds its own work done.

// Intent is a plan as it is about to run in Dir by process PID.
type Intent struct {
	Dir  string    `json:"dir"`
	PID  int       `json:"pid"`
	Plan Plan      `json:"plan"`
	Ops  []Planned `json:"ops"` // one for each op of Plan
}

// Planned is what Recover needs of an op besides the op itself.
type Planned struct {
	// ID is the entry an op moves, as it is before: a delete's, a
	// rename's. Zero for others.
	ID     FileID `json:"id,omitzero"`
	Before FileID `json:"before,omitzero"`
	// Temp is a rename's temporary name, between its two phases.
	Temp string `json:"temp,omitempty"`
	// Made is the top folder the op will make on the way, if any.
	Made string `json:"made,omitempty"`
}

// Prepare returns the Intent of running p in dir now. It reads the disk,
// for identities and for the folders not there yet, and changes nothing.
func Prepare(dir string, p Plan) Intent {
	in := Intent{Dir: dir, PID: os.Getpid(), Plan: p, Ops: make([]Planned, len(p))}
	renames := 0
	for i, op := range p {
		o := &in.Ops[i]
		switch op.Kind {
		case OpDelete:
			o.ID, _ = idOf(filepath.Join(dir, op.From)) //nolint:errcheck // gone: check refuses the plan
		case OpRename:
			o.ID, _ = idOf(filepath.Join(dir, op.From)) //nolint:errcheck // as above
			o.Temp = filepath.Join(dir, ".fndr-rename-"+strconv.Itoa(in.PID)+"-"+strconv.Itoa(renames))
			renames++
			o.Made = madeIn(dir, Target(dir, op.To))
		case OpCopy:
			to := Target(dir, op.To)
			o.Made = madeIn(dir, to)
			o.Before, _ = idOf(to) //nolint:errcheck // an absent destination has no identity
		case OpCreate:
			to := filepath.Join(dir, filepath.Clean(op.To))
			o.Made = madeIn(dir, to)
			o.Before, _ = idOf(to) //nolint:errcheck // an absent destination has no identity
		}
	}
	return in
}

// Recover returns the steps of in that are done on the disk, as Run would
// have, after finishing what a crash left halfway (see above): fixed is
// how many entries it moved from a temporary name. err names what it
// could not place; the rest is recovered all the same.
func Recover(in Intent, trash *Trash) (steps []Step, fixed int, err error) {
	var failed []string
	swept := map[string]bool{}
	dir := in.Dir
	for i, op := range in.Plan {
		o := in.Ops[i]
		switch op.Kind {
		case OpDelete:
			from := filepath.Join(dir, op.From)
			if is(from, o.ID) {
				continue // not done: no need to read the whole trash to find it is not there
			}
			if at := trash.find(o.ID); at != "" {
				steps = append(steps, Step{Kind: OpDelete, From: from, To: at, ID: o.ID})
			}
		case OpCopy:
			from, to := Target(dir, op.From), Target(dir, op.To)
			if d := filepath.Dir(to); !swept[d] {
				swept[d] = true
				sweepCopies(d, in.PID)
			}
			if id, err := idOf(to); err == nil {
				if id != o.Before {
					steps = append(steps, Step{Kind: OpCopy, From: from, To: to, Made: o.Made, ID: id})
				}
			} else {
				removeEmpty(filepath.Dir(to), o.Made) // made on the way to a copy that never got there
			}
		case OpRename:
			from, to := filepath.Join(dir, op.From), Target(dir, op.To)
			switch {
			case is(to, o.ID):
				steps = append(steps, Step{Kind: OpRename, From: from, To: to, Made: o.Made, ID: o.ID})
			case is(o.Temp, o.ID):
				switch {
				case os.MkdirAll(filepath.Dir(to), 0o755) == nil && noReplace(o.Temp, to) == nil: //nolint:gosec // the user's own folders
					steps = append(steps, Step{Kind: OpRename, From: from, To: to, Made: o.Made, ID: o.ID})
					fixed++
				case noReplace(o.Temp, from) == nil:
					removeEmpty(filepath.Dir(to), o.Made)
					fixed++
				default:
					failed = append(failed, fmt.Sprintf("%q is left as %q", op.From, filepath.Base(o.Temp)))
				}
			}
		case OpCreate:
			to := filepath.Join(dir, filepath.Clean(op.To))
			if id, err := idOf(to); err == nil {
				if id != o.Before {
					steps = append(steps, Step{Kind: OpCreate, To: to, Made: o.Made, ID: id})
				}
			} else {
				removeEmpty(filepath.Dir(to), o.Made)
			}
		}
	}
	if len(failed) > 0 {
		return steps, fixed, errors.New(strings.Join(failed, ", "))
	}
	return steps, fixed, nil
}

// is reports whether the file at path is id's.
func is(path string, id FileID) bool {
	got, err := idOf(path)
	return err == nil && got == id // a zero id is no file's
}

// find returns where in the trash the entry id is, or "". The trash is
// read whole: it is only after a crash, and a rename keeps no other trace
// of the name it chose.
func (t *Trash) find(id FileID) string {
	if t == nil {
		return ""
	}
	es, _ := os.ReadDir(t.files) //nolint:errcheck // no trash: nothing found
	for _, e := range es {
		if p := filepath.Join(t.files, e.Name()); is(p, id) {
			return p
		}
	}
	return ""
}

// copyPrefix starts the temporary names of the copies of process pid.
func copyPrefix(pid int) string { return ".fndr-copy-" + strconv.Itoa(pid) + "-" }

// sweepCopies discards, in dir, the copies pid was making when it was cut
// off: under their temporary names, they never were the user's.
func sweepCopies(dir string, pid int) {
	es, _ := os.ReadDir(dir) //nolint:errcheck // nothing to sweep
	for _, e := range es {
		if strings.HasPrefix(e.Name(), copyPrefix(pid)) {
			discard(filepath.Join(dir, e.Name()))
		}
	}
}

// crashPoint is called after each change Run makes to the disk. It does
// nothing; a test sets it to stop Run there, as a crash would, to check
// that Recover makes sense of the disk wherever that happens.
var crashPoint = func() {}
