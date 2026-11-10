package oil

import (
	"errors"
	"slices"
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

// typeKeys presses each character of s as a key: typeKeys(b, "3j"). A
// few control characters stand for the keys that have names, so a whole
// edit reads as typed: "Anew\x1b".
func typeKeys(b *Buffer, s string) {
	_, _ = press(b, s)
}

// press types s like typeKeys and returns what the last key asked for.
func press(b *Buffer, s string) (act Action, err error) {
	for _, r := range s {
		act, err = b.Key(keyName(r))
	}
	return act, err
}

func keyName(r rune) string {
	switch r {
	case '\x1b':
		return "esc"
	case '\b':
		return "backspace"
	case '\n':
		return "enter"
	case ' ':
		return "space"
	}
	return string(r)
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
	for _, k := range []string{"3", "down", "up"} {
		_, _ = b.Key(k)
	}
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
		_, _ = b.Key(k)
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
	for _, s := range []string{"dd", "2d3jp", "yGP", "gg9G", "dGp", "jjdk3P", "jx", "jjj$xp", "3lD", "y$P", "Anew\x1b", "ccx\x1bj", "o\b\b\x1b", "li\n\x1b", "d$u", "xuU"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 64 {
			return // long inputs only repeat what short ones do, slower
		}
		b := lines("hello", "и\u0306ога", "", "日本.txt", "a")
		before := texts(b)
		for _, r := range s {
			tick, was := b.Tick(), texts(b)
			_, _ = b.Key(keyName(r))
			if b.Tick() == tick && texts(b) != was {
				// The interface trusts the tick to skip work: an edit it
				// does not see would be dropped by :q without a word.
				t.Fatalf("after %q: lines changed to %q, tick did not move", s, texts(b))
			}
			if len(b.lines) == 0 || b.row < 0 || b.row >= len(b.lines) {
				t.Fatalf("after %q: %d lines, row %d", s, len(b.lines), b.row)
			}
			limit := lastChar(b.text())
			if b.mode == Insert {
				limit = len(b.text())
			}
			if line := b.text(); b.col < 0 || b.col > limit || b.col < len(line) && prevChar(line, nextChar(line, b.col)) != b.col {
				t.Fatalf("after %q: col %d in %q is not on a character", s, b.col, line)
			}
		}
		if !strings.ContainsAny(s, "dpPxDcCsSiaIAoOuU") && texts(b) != before {
			t.Fatalf("%q changed the lines to %q without a command that edits", s, texts(b))
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

// joined returns the buffer's lines joined with "|": "hello|world".
func joined(b *Buffer) string {
	ls := b.Lines()
	texts := make([]string, len(ls))
	for i, l := range ls {
		texts[i] = l.Text
	}
	return strings.Join(texts, "|")
}

// TestInsert: typing text. Each case types keys on the lines "hello" and
// "world", with the cursor starting on the "e" of hello (col 1), and
// expects the lines after, joined with "|", and the cursor.
func TestInsert(t *testing.T) {
	tests := []struct {
		keys, want string
		row, col   int
		mode       Mode
	}{
		{"iX\x1b", "hXello|world", 0, 1, Normal}, // esc steps back onto what was typed
		{"aX\x1b", "heXllo|world", 0, 2, Normal},
		{"IX\x1b", "Xhello|world", 0, 0, Normal},
		{"AX\x1b", "helloX|world", 0, 5, Normal},
		{"A.md\x1b", "hello.md|world", 0, 7, Normal},
		{"i\x1b", "hello|world", 0, 0, Normal},
		{"iab", "habello|world", 0, 3, Insert},
		{"A", "hello|world", 0, 5, Insert}, // past the last character, where typing appends
		{"i a\x1b", "h aello|world", 0, 2, Normal},
		{"iю\x1b", "hюello|world", 0, 1, Normal},

		{"onew\x1b", "hello|new|world", 1, 2, Normal},
		{"Onew\x1b", "new|hello|world", 0, 2, Normal},
		{"jo\x1b", "hello|world|", 2, 0, Normal},

		{"i\b\x1b", "ello|world", 0, 0, Normal},
		{"A\b\b\x1b", "hel|world", 0, 2, Normal},
		{"ji\b\x1b", "hello|orld", 1, 0, Normal}, // the cursor was on the o: w is erased
		{"jI\b\x1b", "helloworld", 0, 4, Normal}, // backspace at the start joins the lines
		{"I\b\x1b", "hello|world", 0, 0, Normal}, // nothing above to join

		{"i\n\x1b", "h|ello|world", 1, 0, Normal}, // enter splits the line
		{"A\nnew\x1b", "hello|new|world", 1, 2, Normal},

		{"ihi\x1bj", "hhiello|world", 1, 2, Normal}, // j keeps the column esc left
	}
	for _, tt := range tests {
		b := lines("hello", "world")
		typeKeys(b, "l")
		typeKeys(b, tt.keys)
		if g := joined(b); g != tt.want || b.Row() != tt.row || b.Col() != tt.col || b.Mode() != tt.mode {
			t.Errorf("%q: %q row %d col %d %v, want %q row %d col %d %v",
				tt.keys, g, b.Row(), b.Col(), b.Mode(), tt.want, tt.row, tt.col, tt.mode)
		}
	}
}

// TestChange: c deletes what its motion covers and starts typing there.
func TestChange(t *testing.T) {
	tests := []struct{ keys, want string }{
		{"ccnotes.md\x1b", "notes.md|world"},
		{"Snotes.md\x1b", "notes.md|world"},
		{"Cy\x1b", "hy|world"}, // from the e to the end
		{"c$y\x1b", "hy|world"},
		{"sa\x1b", "hallo|world"},
		{"3sa\x1b", "hao|world"}, // three characters: e, l, l
		{"c0j\x1b", "jello|world"},
		{"cjx\x1b", "x"}, // two lines become one
		{"Gccx\x1b", "hello|x"},
		{"$sX\x1b", "hellX|world"}, // the last character
	}
	for _, tt := range tests {
		b := lines("hello", "world")
		typeKeys(b, "l")
		typeKeys(b, tt.keys)
		if g := joined(b); g != tt.want || b.Mode() != Normal {
			t.Errorf("%q: %q %v, want %q NORMAL", tt.keys, g, b.Mode(), tt.want)
		}
	}
}

// TestRenameAndCreateLines: what the plan will read. A changed name keeps
// its id (rename); a typed line has none (create); a split leaves the
// tail without one.
func TestRenameAndCreateLines(t *testing.T) {
	b := lines("notes.md", "main.go")
	typeKeys(b, "ccideas.md\x1bobin/run.sh\x1b")
	want := []mutation.Line{{ID: 1, Text: "ideas.md"}, {ID: 0, Text: "bin/run.sh"}, {ID: 2, Text: "main.go"}}
	if got := b.Lines(); !slices.Equal(got, want) {
		t.Errorf("lines %+v, want %+v", got, want)
	}
}

// TestEmptyLineStillTypes: an empty folder is one blank line, and cl, a, A
// and i on it all start typing.
func TestEmptyLineStillTypes(t *testing.T) {
	for _, keys := range []string{"sx\x1b", "ax\x1b", "Ax\x1b", "ix\x1b", "ccx\x1b"} {
		b := New(nil)
		typeKeys(b, keys)
		if got := b.Lines(); len(got) != 1 || got[0].Text != "x" {
			t.Errorf("%q on an empty buffer: %+v, want one line x", keys, got)
		}
	}
}

func TestUndo(t *testing.T) {
	tests := []struct {
		keys, want string // keys typed on lines a..e; the lines after
		row        int
	}{
		{"u", "abcde", 0}, // nothing to undo
		{"ddu", "abcde", 0},
		{"jjddu", "abcde", 2}, // the cursor goes back where the change was
		{"3ddu", "abcde", 0},  // one command, one step
		{"ddddu", "bcde", 0},
		{"dddd2u", "abcde", 0},
		{"Ixyz\x1bu", "abcde", 0}, // all the typing of one insert is one step
		{"Ixyz\x1bAw\x1bu", "xyzabcde", 0},
		{"ddi\x1bu", "abcde", 0}, // an insert that typed nothing is no step
		{"ddyyu", "abcde", 0},    // nor is a yank
		{"ddd0u", "abcde", 0},    // nor a delete of nothing
		{"Go\x1bu", "abcde", 4},  // o on the last line, undone: back on that line
		{"dduU", "bcde", 0},
		{"ddjdduuUU", "bde", 1},
		{"ddux", "bcde", 0},  // x erases a's only letter
		{"dduxU", "bcde", 0}, // and a new change ends what U could redo
	}
	for _, tt := range tests {
		b := folder(5)
		typeKeys(b, tt.keys)
		if texts(b) != tt.want || b.Row() != tt.row {
			t.Errorf("%q: lines %q row %d, want %q row %d", tt.keys, texts(b), b.Row(), tt.want, tt.row)
		}
	}
}

// TestUndoKeepsIDs: an undone delete brings the entry back, not a new
// line with the same name, or the plan would trash it and create another.
func TestUndoKeepsIDs(t *testing.T) {
	b := folder(3)
	typeKeys(b, "dGu")
	if got := b.Lines(); got[0].ID != 1 || got[2].ID != 3 {
		t.Errorf("lines %+v, want ids 1..3 back", got)
	}
}

func TestUndoIsBounded(t *testing.T) {
	b := folder(5)
	for range maxUndo + 10 {
		typeKeys(b, "x")
		typeKeys(b, "P")
	}
	if len(b.undo) > maxUndo {
		t.Errorf("%d undo steps kept, want at most %d", len(b.undo), maxUndo)
	}
}

func TestActions(t *testing.T) {
	tests := []struct {
		keys string
		want Action
	}{
		{":w\n", Write},
		{":q\n", Quit},
		{":q!\n", ForceQuit},
		{":wq\n", WriteQuit},
		{":x\n", WriteQuit},
		{": w \n", Write},
		{"ZZ", WriteQuit},
		{"ZQ", ForceQuit},
		{"q", Close},
		{"\x1b", Close},
		{"3\x1b", None}, // esc drops the count and no more, as in Vim
		{"d\x1b", None}, // or the operator
		{"g\x1b", None}, // or the prefix
		{"i\x1b", None}, // or Insert mode: the second esc closes
		{"i\x1b\x1b", Close},
		{"\n", Open},
		{"-", Parent},
		{"_", Cwd},
		{"g.", ToggleHidden},
		{"gp", CopyPath},
		{"g?", Help},
		{"j", None},
		{"dq", None}, // after an operator, q is no command
		{"d\n", None},
		{"Zx", None},
		{":\x1b", None},
		{":w\x1b", None}, // esc drops the line
		{":\n", None},
	}
	if act, _ := folder(5).Key("ctrl+o"); act != Close {
		t.Errorf("ctrl+o: %v, want Close: the key that opens OIL closes it", act)
	}
	for _, tt := range tests {
		b := folder(5)
		act, err := press(b, tt.keys)
		if act != tt.want || err != nil {
			t.Errorf("%q: %v %v, want %v", tt.keys, act, err, tt.want)
		}
		if b.Mode() != Normal {
			t.Errorf("%q: left in %v", tt.keys, b.Mode())
		}
	}
	if act, err := folder(1).Key("ctrl+r"); act != Refresh || err != nil {
		t.Errorf("ctrl+r: %v %v, want Refresh: it reads the folder again, as in Oil.nvim", act, err)
	}
}

func TestCommandLine(t *testing.T) {
	b := folder(5)
	typeKeys(b, ":wq")
	if b.Mode() != Command || b.CmdLine() != "wq" {
		t.Errorf("mode %v line %q, want COMMAND with wq", b.Mode(), b.CmdLine())
	}
	typeKeys(b, "\b\b")
	if b.Mode() != Command || b.CmdLine() != "" {
		t.Errorf("after two backspaces: mode %v line %q", b.Mode(), b.CmdLine())
	}
	typeKeys(b, "\b") // on an empty line: back to Normal
	if b.Mode() != Normal {
		t.Errorf("mode %v, want NORMAL", b.Mode())
	}

	if _, err := press(b, ":rm -rf\n"); !errors.Is(err, ErrCommand) {
		t.Errorf("err %v, want ErrCommand", err)
	}
	typeKeys(b, ":3\n")
	if b.Row() != 2 {
		t.Errorf(":3 put the cursor on row %d, want 2", b.Row())
	}
	typeKeys(b, ":99\n")
	if b.Row() != 4 {
		t.Errorf(":99 put the cursor on row %d, want the last", b.Row())
	}
}
