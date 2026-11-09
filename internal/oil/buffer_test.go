package oil

import (
	"strings"
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

// texts returns the buffer's lines joined: "abcde" for lines a..e.
func texts(b *Buffer) string {
	var s strings.Builder
	for _, l := range b.Lines() {
		s.WriteString(l.Text)
	}
	return s.String()
}

func TestCommands(t *testing.T) {
	tests := []struct {
		keys, want string // keys typed on lines a..e; the lines after
		row        int
	}{
		// Motions of their own.
		{"G", "abcde", 4},
		{"Ggg", "abcde", 0},
		{"3G", "abcde", 2},
		{"2gg", "abcde", 1},
		{"9G", "abcde", 4},  // past the end: the last line
		{"gxj", "abcde", 1}, // g then a key that is no command: both dropped

		// Delete.
		{"dd", "bcde", 0},
		{"jdd", "acde", 1},
		{"Gdd", "abcd", 3}, // the cursor stays on the new last line
		{"2dd", "cde", 0},
		{"d2d", "cde", 0},
		{"9dd", "", 0}, // as many as there are; the buffer keeps a blank line
		{"Gk2dd", "abc", 2},
		{"dj", "cde", 0},
		{"jjdk", "ade", 1},
		{"2d2j", "", 0}, // "d4j": counts multiply
		{"jjdgg", "de", 0},
		{"jdG", "a", 0},
		{"d3G", "de", 0},
		{"Gdj", "abcde", 4}, // j cannot move: nothing is deleted
		{"dk", "abcde", 0},

		// Yank and put.
		{"yyp", "aabcde", 1},
		{"yyP", "aabcde", 0},
		{"jykP", "ababcde", 0}, // yanking upwards takes the cursor along
		{"ddp", "bacde", 1},
		{"ddP", "abcde", 0},
		{"dd3p", "baaacde", 1},
		{"Gyy2P", "abcdeee", 4},
		{"p", "abcde", 0}, // nothing yanked yet

		// What pending keys become when a command is not finished.
		{"dxj", "abcde", 1},
		{"d0j", "abcde", 1}, // "0" is no count here, and no motion yet
		{"y2y", "abcde", 0},
	}
	for _, tt := range tests {
		b := folder(5)
		typeKeys(b, tt.keys)
		if texts(b) != tt.want || b.Row() != tt.row {
			t.Errorf("%q: lines %q row %d, want %q row %d", tt.keys, texts(b), b.Row(), tt.want, tt.row)
		}
	}
}

// TestEscCancels: esc drops a half-typed command, and the keys after it
// mean what they mean on their own.
func TestEscCancels(t *testing.T) {
	b := folder(5)
	for _, k := range []string{"3", "d", "esc", "j"} {
		b.Key(k)
	}
	if texts(b) != "abcde" || b.Row() != 1 {
		t.Errorf("lines %q row %d after 3d<esc>j, want nothing deleted and row 1", texts(b), b.Row())
	}
}

// TestMovedLineKeepsItsID: a line deleted and put back elsewhere is the
// same entry, which is what makes dd and p a move rather than a delete and
// a create.
func TestMovedLineKeepsItsID(t *testing.T) {
	b := folder(5)
	typeKeys(b, "ddjp") // a, put back below c
	lines := b.Lines()
	if texts(b) != "bcade" || lines[2].ID != 1 {
		t.Errorf("lines %v, want a with id 1 on the third line", lines)
	}
}

// TestDeletingEverythingLeavesOneBlankLine: the buffer is never empty, and
// what was deleted can still be put back.
func TestDeletingEverythingLeavesOneBlankLine(t *testing.T) {
	b := folder(3)
	typeKeys(b, "dGp")
	if got := b.Lines(); len(got) != 4 || got[0] != (mutation.Line{}) || texts(b) != "abc" {
		t.Errorf("lines %v, want the blank line and a, b, c put below it", got)
	}
}

func TestHugePutIsCapped(t *testing.T) {
	b := folder(5)
	typeKeys(b, "yG99999p")
	if n := len(b.Lines()); n > maxCount+5 {
		t.Errorf("%d lines: a count must not fill the memory", n)
	}
}

// FuzzKeys: whatever is typed, the buffer keeps its invariants: never
// empty, the cursor on a line, and no line lost or made up but by the keys
// that delete and put.
func FuzzKeys(f *testing.F) {
	for _, s := range []string{"dd", "2d3jp", "yGP", "gg9G", "dGp", "jjdk3P", "jx", "jjj$xp", "3lD", "y$P"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 64 {
			return // long inputs only repeat what short ones do, slower
		}
		b := lines("hello", "и\u0306ога", "", "日本.txt", "a")
		before := texts(b)
		for _, r := range s {
			b.Key(string(r))
			if len(b.lines) == 0 || b.row < 0 || b.row >= len(b.lines) {
				t.Fatalf("after %q: %d lines, row %d", s, len(b.lines), b.row)
			}
			if line := b.text(); b.col < 0 || b.col > lastChar(line) || prevChar(line, nextChar(line, b.col)) != b.col {
				t.Fatalf("after %q: col %d in %q is not on a character", s, b.col, line)
			}
		}
		if !strings.ContainsAny(s, "dpPxD") && texts(b) != before {
			t.Fatalf("%q changed the lines to %q without d, p, P, x or D", s, texts(b))
		}
	})
}

// lines returns a buffer of the given lines, with ids 1, 2, 3...
func lines(texts ...string) *Buffer {
	ls := make([]mutation.Line, len(texts))
	for i, t := range texts {
		ls[i] = mutation.Line{ID: i + 1, Text: t}
	}
	return New(ls)
}

// TestInsideALine: motions and edits within the cursor's line. The cursor
// stops on the last character, never past it, as in Vim's normal mode.
func TestInsideALine(t *testing.T) {
	tests := []struct {
		keys, want string // keys typed on the line "hello"; the line after
		col        int
	}{
		{"l", "hello", 1},
		{"3l", "hello", 3},
		{"9l", "hello", 4},
		{"$", "hello", 4},
		{"$0", "hello", 0},
		{"$h", "hello", 3},
		{"h", "hello", 0},
		{"^", "hello", 0},

		{"x", "ello", 0},
		{"3x", "lo", 0},
		{"$x", "hell", 3}, // the cursor steps back onto the new last character
		{"9x", "", 0},
		{"xp", "ehllo", 1}, // swap two letters
		{"$xp", "hello", 4},
		{"lllD", "hel", 2},
		{"lld$", "he", 1},
		{"lld0", "llo", 0},
		{"lldh", "hllo", 1},
		{"dh", "hello", 0}, // nothing before the cursor
		{"ylP", "hhello", 0},
		{"y$$p", "hellohello", 9},
		{"x3p", "ehhhllo", 3},
		{"dx", "hello", 0}, // x is no motion
	}
	for _, tt := range tests {
		b := lines("hello")
		typeKeys(b, tt.keys)
		if got := b.text(); got != tt.want || b.Col() != tt.col {
			t.Errorf("%q: line %q col %d, want %q col %d", tt.keys, got, b.Col(), tt.want, tt.col)
		}
	}
}

// TestEditKeepsID: changing a name keeps the line's id, which
// mutation.Diff reads as a rename.
func TestEditKeepsID(t *testing.T) {
	b := lines("notes.md", "main.go")
	typeKeys(b, "jx")
	if got := b.Lines()[1]; got != (mutation.Line{ID: 2, Text: "ain.go"}) {
		t.Errorf("line %+v, want id 2 kept", got)
	}
}

// TestColumnSurvivesShortLines: j and k aim for the column the cursor was
// put in, through a line too short to hold it.
func TestColumnSurvivesShortLines(t *testing.T) {
	tests := []struct {
		keys string
		col  int
	}{
		{"4lj", 1},  // "ab" is short: its last character
		{"4ljj", 4}, // and back to column 4 on "world"
		{"$jj", 4},  // $ keeps to the end of every line
		{"$j", 1},
		{"4ljhj", 0}, // a horizontal move sets a new column
		{"4ldd", 0},  // deleting a line puts the cursor at its start
	}
	for _, tt := range tests {
		b := lines("hello", "ab", "world")
		typeKeys(b, tt.keys)
		if b.Col() != tt.col {
			t.Errorf("%q: col %d, want %d", tt.keys, b.Col(), tt.col)
		}
	}
}

// TestDecomposedLetters: macOS may store "й" as "и" and a combining
// breve. To the cursor that is one letter, as it is to the eye.
func TestDecomposedLetters(t *testing.T) {
	const yoga = "и\u0306ога" // йога, decomposed
	b := lines(yoga)
	typeKeys(b, "l")
	if want := len("и\u0306"); b.Col() != want {
		t.Errorf("l: col %d, want %d, after the whole й", b.Col(), want)
	}
	b = lines(yoga)
	typeKeys(b, "x")
	if b.text() != "ога" {
		t.Errorf("x: %q, want the whole й erased", b.text())
	}
	b = lines(yoga)
	typeKeys(b, "$hhh")
	if b.Col() != 0 {
		t.Errorf("$hhh: col %d, want 0", b.Col())
	}
}
