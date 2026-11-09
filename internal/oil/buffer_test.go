package oil

import (
	"testing"

	"github.com/ValeryCherneykin/fndr/internal/mutation"
)

// folder returns a buffer of n lines named a, b, c..., with ids 1..n.
func folder(n int) *Buffer {
	lines := make([]mutation.Line, n)
	for i := range lines {
		lines[i] = mutation.Line{ID: i + 1, Text: string(rune('a' + i))}
	}
	return New(lines)
}

// typeKeys presses each character of s as a key: typeKeys(b, "3j").
func typeKeys(b *Buffer, s string) {
	for _, r := range s {
		b.Key(string(r))
	}
}

func TestEmptyFolderIsOneBlankLine(t *testing.T) {
	b := New(nil)
	if got := b.Lines(); len(got) != 1 || got[0] != (mutation.Line{}) {
		t.Errorf("lines %v, want one blank line", got)
	}
	typeKeys(b, "jk5j")
	if b.Row() != 0 {
		t.Errorf("row %d in a one-line buffer", b.Row())
	}
}

func TestMoveUpAndDown(t *testing.T) {
	tests := []struct {
		keys string
		want int
	}{
		{"", 0},
		{"j", 1},
		{"jj", 2},
		{"jjk", 1},
		{"k", 0},           // stops at the top
		{"jjjjjjjjjjj", 9}, // and at the bottom
		{"3j", 3},
		{"3jk", 2}, // the count is used up by the first move
		{"12j", 9}, // counts of several digits, clamped
		{"5j2k", 3},
		{"0j", 1},  // "0" alone is not a count
		{"10j", 9}, // but a 0 after a digit is
		{"3xj", 1}, // an unknown key drops the count
	}
	for _, tt := range tests {
		b := folder(10)
		typeKeys(b, tt.keys)
		if b.Row() != tt.want {
			t.Errorf("%q: row %d, want %d", tt.keys, b.Row(), tt.want)
		}
	}
}

func TestArrowsMoveToo(t *testing.T) {
	b := folder(5)
	b.Key("3")
	b.Key("down")
	b.Key("up")
	if b.Row() != 2 {
		t.Errorf("row %d after 3↓↑, want 2", b.Row())
	}
}

func TestHugeCountDoesNotOverflow(t *testing.T) {
	b := folder(5)
	typeKeys(b, "99999999999999999999j")
	if b.Row() != 4 {
		t.Errorf("row %d, want the last line", b.Row())
	}
	typeKeys(b, "99999999999999999999k")
	if b.Row() != 0 {
		t.Errorf("row %d, want the first line", b.Row())
	}
}

// TestLinesAreCopies: the buffer edits its own lines. Changing what went
// in, or what came out, must not change the buffer behind its back.
func TestLinesAreCopies(t *testing.T) {
	in := []mutation.Line{{ID: 1, Text: "a"}}
	b := New(in)
	in[0].Text = "changed"
	out := b.Lines()
	out[0].Text = "changed too"
	if got := b.Lines()[0].Text; got != "a" {
		t.Errorf("buffer line %q, want %q", got, "a")
	}
}
