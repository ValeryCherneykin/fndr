package mutation

import (
	"errors"
	"fmt"
	"strings"
)

// A buffer edit is turned into changes to the disk in two steps: Diff
// compares the folder as it was shown with the lines the user wrote and
// returns a Plan; Apply carries it out. Diff only reads its arguments, so
// everything that can be wrong with an edit is found before anything on the
// disk changes, and the plan can be shown for confirmation first.
//
// What a line means hangs on its ID, which the buffer keeps out of sight
// (as Oil.nvim conceals its ids):
//
//	ID of an item, same name    nothing happens
//	ID of an item, new name     rename (a path moves it: "old/x.go")
//	ID 0                        create, by Create's rules ("a/b/", "x.txt")
//	ID on no line               delete: the item goes to the trash
//
// Names are relative to the folder; a folder's name may carry a trailing
// "/", as the buffer shows it, which a rename ignores: it cannot turn a
// folder into a file.

// Item is an entry of the folder as the buffer was filled with it.
type Item struct {
	ID   int // > 0, unique within the folder
	Name string
}

// Line is a line of the buffer as the user left it.
type Line struct {
	ID   int // the Item it came from, or 0 for a line the user added
	Text string
}

// Errors of Diff, wrapped in a *LineError where a line is to blame.
var (
	ErrCopy     = errors.New("the same item is on two lines; copying is not supported yet")
	ErrConflict = errors.New("two entries would have the same name")
	ErrUnknown  = errors.New("line refers to no item of this folder")
)

// LineError says which line of the buffer is wrong, counting from 1, so
// the interface can put the cursor on it.
type LineError struct {
	Line int
	Err  error
}

func (e *LineError) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }
func (e *LineError) Unwrap() error { return e.Err }

// Kind is what an Op does.
type Kind int

// Kinds of Op, in the order Apply runs them: things leave first, so that a
// name freed by a delete or a rename can be taken by a later step.
const (
	OpDelete Kind = iota
	OpRename
	OpCreate
)

// Op is one change. From is the old name (OpDelete, OpRename), To the new
// one (OpRename, OpCreate).
type Op struct {
	Kind     Kind
	From, To string
}

// Plan is the list of changes, deletes first, then renames, then creates.
// An empty plan means the buffer was not changed.
type Plan []Op

// Diff returns the plan that turns items into lines.
func Diff(items []Item, lines []Line) (Plan, error) {
	byID := make(map[int]string, len(items))
	shown := make(map[int]string, len(items)) // the line as the buffer was filled
	for _, it := range items {
		byID[it.ID] = strings.TrimSuffix(it.Name, "/")
		shown[it.ID] = it.Name
	}

	var renames, creates Plan
	seen := make(map[int]bool, len(lines))
	taken := map[string]int{} // final name → the line that claims it
	for i, l := range lines {
		n := i + 1
		if name, ok := shown[l.ID]; ok && l.Text == name && !seen[l.ID] {
			// Untouched: the entry stays as it is, whatever its name. What
			// follows cleans what the user typed, and would rename "notes "
			// to "notes", or macOS's "Icon\r" (a folder's custom icon) to
			// "Icon", or read a file named " " as a blank line, to delete.
			seen[l.ID] = true
			old := byID[l.ID]
			if prev, dup := taken[old]; dup {
				return nil, &LineError{Line: n, Err: fmt.Errorf("%w: %q, as on line %d", ErrConflict, old, prev)}
			}
			taken[old] = n
			continue
		}
		text := strings.TrimSpace(l.Text)
		if text == "" {
			continue // a blank line is just spacing, as in any editor
		}
		lineErr := func(err error) error { return &LineError{Line: n, Err: err} }

		var op Op
		if l.ID == 0 {
			op = Op{Kind: OpCreate, To: text}
		} else {
			old, ok := byID[l.ID]
			switch {
			case !ok:
				return nil, lineErr(ErrUnknown)
			case seen[l.ID]:
				return nil, lineErr(ErrCopy)
			}
			seen[l.ID] = true
			op = Op{Kind: OpRename, From: old, To: strings.TrimSuffix(text, "/")}
		}

		// The name must be one Create would accept, whatever the line does:
		// a rename to "../x" leaves the folder just as a create would.
		r, err := resolve("", op.To)
		if err != nil {
			return nil, lineErr(err)
		}
		// Two lines may not end on one name: a rename would replace the
		// other entry. resolve cleans, so "a//b" and "a/b" are one name.
		final := r.Path
		if prev, dup := taken[final]; dup {
			return nil, lineErr(fmt.Errorf("%w: %q, as on line %d", ErrConflict, final, prev))
		}
		taken[final] = n

		switch {
		case op.Kind == OpCreate:
			creates = append(creates, op)
		case final != op.From:
			op.To = final
			renames = append(renames, op)
		}
	}

	// Whatever has no line left is deleted, in the folder's order.
	var deletes Plan
	for _, it := range items {
		if !seen[it.ID] {
			deletes = append(deletes, Op{Kind: OpDelete, From: byID[it.ID]})
		}
	}
	return append(append(deletes, renames...), creates...), nil
}
