package mutation

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// folder is the buffer fndr would show for a folder with src/, main.go and
// README.md.
var folder = []Item{{1, "src/"}, {2, "main.go"}, {3, "README.md"}}

// untouched is that buffer as it was filled.
func untouched() []Line {
	return []Line{{1, "src/"}, {2, "main.go"}, {3, "README.md"}}
}

func TestDiffUnchanged(t *testing.T) {
	p, err := Diff(folder, untouched())
	if err != nil || len(p) != 0 {
		t.Errorf("plan %v, err %v: want nothing to do", p, err)
	}
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name  string
		lines []Line
		want  Plan
	}{
		{"rename", []Line{{1, "src/"}, {2, "app.go"}, {3, "README.md"}},
			Plan{{Kind: OpRename, From: "main.go", To: "app.go"}}},
		{"rename a folder, slash or not", []Line{{1, "lib"}, {2, "main.go"}, {3, "README.md"}},
			Plan{{Kind: OpRename, From: "src", To: "lib"}}},
		{"dd deletes", []Line{{1, "src/"}, {3, "README.md"}},
			Plan{{Kind: OpDelete, From: "main.go"}}},
		{"a new line creates, folders and all", append(untouched(), Line{0, "bin/main.txt"}, Line{0, "docs/"}),
			Plan{{Kind: OpCreate, To: "bin/main.txt"}, {Kind: OpCreate, To: "docs/"}}},
		{"moved lines are not changes", []Line{{3, "README.md"}, {1, "src/"}, {2, "main.go"}}, nil},
		{"blank lines are spacing", append(untouched(), Line{0, "   "}), nil},
		{"a path moves", []Line{{1, "src/"}, {2, "src/main.go"}, {3, "README.md"}},
			Plan{{Kind: OpRename, From: "main.go", To: "src/main.go"}}},
		{"a freed name can be taken", []Line{{1, "src/"}, {3, "main.go"}},
			Plan{{Kind: OpDelete, From: "main.go"}, {Kind: OpRename, From: "README.md", To: "main.go"}}},
		{"a swap is allowed: Apply does it in two phases", []Line{{1, "src/"}, {2, "README.md"}, {3, "main.go"}},
			Plan{{Kind: OpRename, From: "main.go", To: "README.md"}, {Kind: OpRename, From: "README.md", To: "main.go"}}},
		// A folder another line goes through is made on the way: created as
		// well, it would be found there and stop Apply halfway, or not,
		// depending on which line came first.
		{"a folder made on the way, after", append(untouched(), Line{0, "docs/a.md"}, Line{0, "docs/"}),
			Plan{{Kind: OpCreate, To: "docs/a.md"}}},
		{"a folder made on the way, before", append(untouched(), Line{0, "docs/"}, Line{0, "docs/a/b.md"}),
			Plan{{Kind: OpCreate, To: "docs/a/b.md"}}},
		{"a folder a rename goes into", []Line{{1, "src/"}, {2, "cmd/main.go"}, {3, "README.md"}, {0, "cmd/"}},
			Plan{{Kind: OpRename, From: "main.go", To: "cmd/main.go"}}},
		{"order: delete, rename, create", []Line{{0, "new.txt"}, {2, "app.go"}, {3, "README.md"}},
			Plan{
				{Kind: OpDelete, From: "src"},
				{Kind: OpRename, From: "main.go", To: "app.go"},
				{Kind: OpCreate, To: "new.txt"},
			}},
	}
	for _, tt := range tests {
		p, err := Diff(folder, tt.lines)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if !slices.Equal(p, tt.want) {
			t.Errorf("%s:\n got %v\nwant %v", tt.name, p, tt.want)
		}
	}
}

// TestDiffRefuses: every mistake is found before the disk is touched, and
// names its line.
func TestDiffRefuses(t *testing.T) {
	tests := []struct {
		name  string
		lines []Line
		line  int
		want  error
	}{
		{"yy p is a copy", append(untouched(), Line{2, "main.go"}), 4, ErrCopy},
		{"two lines, one name", []Line{{1, "src/"}, {2, "README.md"}, {3, "README.md"}}, 3, ErrConflict},
		{"a new line on an old name", append(untouched(), Line{0, "main.go"}), 4, ErrConflict},
		{"same name, different spelling", append(untouched(), Line{0, "src//"}), 4, ErrConflict},
		{"out of the folder", []Line{{1, "../src"}, {2, "main.go"}, {3, "README.md"}}, 1, ErrOutside},
		{"an id from nowhere", append(untouched(), Line{9, "x"}), 4, ErrUnknown},
	}
	for _, tt := range tests {
		_, err := Diff(folder, tt.lines)
		var le *LineError
		if !errors.As(err, &le) || le.Line != tt.line || !errors.Is(err, tt.want) {
			t.Errorf("%s: %v, want %v on line %d", tt.name, err, tt.want, tt.line)
		}
	}
}

// TestDiffOddNamesStayPut: a line left as it was filled is no change,
// whatever the name. Trimming is for what the user types: a name on disk
// with spaces at its ends, or macOS's "Icon\r" (a folder's custom icon),
// must not be renamed, nor " " trashed as a blank line, because another
// line was edited.
func TestDiffOddNamesStayPut(t *testing.T) {
	items := []Item{{1, "Icon\r"}, {2, "notes "}, {3, " "}, {4, " lead/"}, {5, "ok"}}
	lines := []Line{{1, "Icon\r"}, {2, "notes "}, {3, " "}, {4, " lead/"}, {5, "ok2"}}
	p, err := Diff(items, lines)
	want := Plan{{Kind: OpRename, From: "ok", To: "ok2"}}
	if err != nil || !slices.Equal(p, want) {
		t.Errorf("plan %v, err %v; want only %v", p, err, want)
	}
	// Typed onto, such a name is cleaned like any other.
	p, err = Diff(items[1:2], []Line{{2, "notes 2 "}})
	if want := (Plan{{Kind: OpRename, From: "notes ", To: "notes 2"}}); err != nil || !slices.Equal(p, want) {
		t.Errorf("plan %v, err %v; want %v", p, err, want)
	}
	// And a typed name may not take an untouched one.
	if _, err = Diff(items[4:], []Line{{5, "ok"}, {0, "ok"}}); !errors.Is(err, ErrConflict) {
		t.Errorf("err %v, want ErrConflict", err)
	}
}

// FuzzDiff: for any names on disk and anything typed, a buffer left as
// it was filled is no change, and editing one line never touches the
// other, nor names anything outside the folder.
func FuzzDiff(f *testing.F) {
	f.Add("a", "b/", "c")
	f.Add("Icon\r", "notes ", " ")
	f.Add(" ", "..", "../x")
	f.Fuzz(func(t *testing.T, a, b, typed string) {
		if a == b || strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/") || a == "" || b == "" {
			return // names in one folder are unique and never empty
		}
		items := []Item{{1, a}, {2, b}}
		p, err := Diff(items, []Line{{1, a}, {2, b}})
		if err != nil || len(p) != 0 {
			t.Fatalf("untouched: plan %v, err %v", p, err)
		}
		p, err = Diff(items, []Line{{1, a}, {2, typed}})
		if err != nil {
			return
		}
		for _, op := range p {
			if op.From == strings.TrimSuffix(a, "/") {
				t.Fatalf("editing line 2 changed line 1: %v", p)
			}
			if op.To != "" && !filepath.IsLocal(filepath.Clean(op.To)) {
				t.Fatalf("%q leaves the folder: %v", op.To, p)
			}
		}
	})
}
