package mutation

import (
	"errors"
	"slices"
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
