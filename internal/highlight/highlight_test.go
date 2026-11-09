package highlight

import (
	"slices"
	"strings"
	"testing"
)

// kinds renders spans as "kind:text" pieces, the readable form for tests.
func kinds(spans []Span) []string {
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = kindNames[s.Kind] + ":" + s.Text
	}
	return out
}

var kindNames = map[Kind]string{
	Plain: "p", Comment: "c", String: "s", Number: "n",
	Keyword: "k", Type: "t", Constant: "const", Func: "f",
}

func TestGoLine(t *testing.T) {
	got := kinds(Lines("main.go", []string{`	if err := run(42, "x"); err != nil { // fail`})[0])
	want := []string{
		"p:\t", "k:if", "p: err := ", "f:run", "p:(", "n:42", "p:, ", `s:"x"`,
		"p:); err != ", "const:nil", "p: { ", "c:// fail",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestWordsAreWholeWords(t *testing.T) {
	// "format" contains "for", "interval" contains "int", "utf8" ends in a
	// digit: none of them may be cut up.
	got := kinds(Lines("x.go", []string{"format interval utf8 x2"})[0])
	if want := []string{"p:format interval utf8 x2"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestQualifiedCall(t *testing.T) {
	got := kinds(Lines("x.go", []string{"fmt.Println(s)"})[0])
	if want := []string{"p:fmt.", "f:Println", "p:(s)"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTypesAndKeywords(t *testing.T) {
	got := kinds(Lines("x.go", []string{"func f(n int) string"})[0])
	want := []string{"k:func", "p: ", "f:f", "p:(n ", "t:int", "p:) ", "t:string"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestBlocksSpanLines: a block comment or raw string colours every line it
// covers, which is why lines are lexed as one text.
func TestBlocksSpanLines(t *testing.T) {
	got := Lines("x.go", []string{"a /* one", "two */ b", "s := `raw", "still raw` + c"})
	want := [][]string{
		{"p:a ", "c:/* one"},
		{"c:two */", "p: b"},
		{"p:s := ", "s:`raw"},
		{"s:still raw`", "p: + c"},
	}
	for i := range want {
		if g := kinds(got[i]); !slices.Equal(g, want[i]) {
			t.Errorf("line %d: got %q, want %q", i, g, want[i])
		}
	}
}

func TestStrings(t *testing.T) {
	tests := map[string][]string{
		`x = "a \" b" + y`:  {"p:x = ", `s:"a \" b"`, "p: + y"}, // escaped quote stays inside
		`x = "never closed`: {"p:x = ", `s:"never closed`},
		`c = '"'`:           {"p:c = ", `s:'"'`},
	}
	for line, want := range tests {
		if got := kinds(Lines("x.go", []string{line})[0]); !slices.Equal(got, want) {
			t.Errorf("%s: got %q, want %q", line, got, want)
		}
	}
}

// TestUnclosedStringStopsAtLineEnd: one stray quote must not paint the rest
// of the file as a string.
func TestUnclosedStringStopsAtLineEnd(t *testing.T) {
	got := Lines("x.py", []string{`print("oops`, "x = 1"})
	if g := kinds(got[1]); !slices.Equal(g, []string{"p:x = ", "n:1"}) {
		t.Errorf("line after a stray quote: %q", g)
	}
}

func TestLanguages(t *testing.T) {
	tests := []struct {
		name, line string
		want       []string
	}{
		{"a.py", "def f(): # hi", []string{"k:def", "p: ", "f:f", "p:(): ", "c:# hi"}},
		{"a.rs", "let x: &'a str", []string{"k:let", "p: x: &'a ", "t:str"}},
		{"a.lua", "--[[ x ]] local", []string{"c:--[[ x ]]", "p: ", "k:local"}},
		{"q.sql", "SELECT id FROM t", []string{"k:SELECT", "p: id ", "k:FROM", "p: t"}},
		{"Dockerfile", "FROM golang # base", []string{"k:FROM", "p: golang ", "c:# base"}},
		{"c.yaml", "on: true # ci", []string{"const:on", "p:: ", "const:true", "p: ", "c:# ci"}},
		{"NOTES.TXT", "if x // y", []string{"p:if x // y"}}, // unknown language: plain
	}
	for _, tt := range tests {
		if got := kinds(Lines(tt.name, []string{tt.line})[0]); !slices.Equal(got, tt.want) {
			t.Errorf("%s %q: got %q, want %q", tt.name, tt.line, got, tt.want)
		}
	}
}

func TestEmptyLinesStayEmpty(t *testing.T) {
	got := Lines("x.go", []string{"a", "", "b"})
	if len(got) != 3 || got[1] != nil {
		t.Errorf("got %v, want three lines with the middle one empty", got)
	}
}

// TestSpansRebuildTheText: highlighting only labels text, it never adds,
// drops or reorders a byte.
func TestSpansRebuildTheText(t *testing.T) {
	lines := []string{"package main", "", "/* a", " b */ func main() {", "\ts := `x", "y` // z", `	fmt.Println("hi", 0x1F)`, "}"}
	for i, spans := range Lines("main.go", lines) {
		var b strings.Builder
		for _, s := range spans {
			b.WriteString(s.Text)
		}
		if b.String() != lines[i] {
			t.Errorf("line %d rebuilt as %q, want %q", i, b.String(), lines[i])
		}
	}
}

func BenchmarkLines(b *testing.B) {
	src := strings.Split(strings.Repeat("func main() {\n\tfmt.Println(\"hello\", 42) // greet\n}\n", 70), "\n")
	for b.Loop() {
		Lines("main.go", src)
	}
}

// BenchmarkLongLine is the worst head a preview can read: 32 KB on one
// line, all punctuation, so every character extends the same span.
func BenchmarkLongLine(b *testing.B) {
	src := []string{strings.Repeat("=", 32<<10)}
	for b.Loop() {
		Lines("main.go", src)
	}
}

// FuzzLines feeds the lexer any text, as the preview does with any file:
// it must never panic, and each line's spans, put back together, must be
// exactly that line, with no empty span and no newline inside one.
func FuzzLines(f *testing.F) {
	for _, s := range []string{
		"func main() {\n\tfmt.Println(\"hi\") // x\n}",
		"/* open\nstill open",
		"--[[ lua\nblock ]] x",
		"s = 'unterminated\nnext",
		"0x1F 1_000 3.14e9 utf8",
		"\"\\",
		"\xff\xfe日本語",
	} {
		f.Add(s)
	}
	names := []string{"main.go", "x.lua", "a.py", "a.sh", "a.json", "Dockerfile", "a.sql", "plain.txt"}
	f.Fuzz(func(t *testing.T, text string) {
		lines := strings.Split(text, "\n")
		for _, name := range names {
			for i, spans := range Lines(name, lines) {
				var b strings.Builder
				for _, sp := range spans {
					if sp.Text == "" || strings.Contains(sp.Text, "\n") {
						t.Fatalf("%s line %d: bad span %q", name, i, sp.Text)
					}
					b.WriteString(sp.Text)
				}
				if b.String() != lines[i] {
					t.Fatalf("%s line %d rebuilt as %q, want %q", name, i, b.String(), lines[i])
				}
			}
		}
	})
}
