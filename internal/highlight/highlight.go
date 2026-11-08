// Package highlight splits source code into coloured spans for the preview
// pane. It returns kinds, not colours: the palette is the TUI's business.
//
// It is one small lexer driven by per-language rules (comment markers,
// string quotes, word lists), not a parser. That is the right trade for a
// preview: it is fast, needs no dependencies, and is right on the code
// people actually look at. A real parser per language would be more exact
// and a hundred times bigger.
package highlight

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind is what a span of code is.
type Kind uint8

// Kinds of span.
const (
	Plain Kind = iota
	Comment
	String
	Number
	Keyword
	Type     // built-in types: int, string, bool
	Constant // true, false, nil
	Func     // a name followed by "(": a call or a definition
)

// Span is a run of text of one kind. Spans never contain a newline.
type Span struct {
	Kind Kind
	Text string
}

// Lines highlights lines of the file called name. Lines are lexed as one
// text, so a comment or raw string spanning several lines is coloured on
// all of them. A file in an unknown language comes back as plain text.
func Lines(name string, lines []string) [][]Span {
	out := make([][]Span, len(lines))
	l := langFor(name)
	if l == nil {
		for i, s := range lines {
			if s != "" {
				out[i] = []Span{{Plain, s}}
			}
		}
		return out
	}
	lx := lexer{lang: l, out: out}
	lx.run(strings.Join(lines, "\n"))
	return out
}

// lexer walks the text once, left to right. At every position it tries the
// rules in order (comments, strings, numbers, words) and emits a span for
// the first that matches; anything else is plain punctuation.
type lexer struct {
	lang *lang
	out  [][]Span
	line int // index into out of the line being written
}

func (lx *lexer) run(s string) {
	for i := 0; i < len(s); {
		i += lx.next(s, i)
	}
}

// next emits the span starting at s[i] and returns its length in bytes.
func (lx *lexer) next(s string, i int) int {
	l, rest := lx.lang, s[i:]

	// Blocks first: Lua's "--[[" block comment starts with its "--" line
	// comment.
	for _, d := range l.blocks {
		if strings.HasPrefix(rest, d.open) {
			return lx.emit(d.kind, rest[:until(rest, len(d.open), d.close)])
		}
	}
	for _, p := range l.lineComments {
		if strings.HasPrefix(rest, p) {
			return lx.emit(Comment, rest[:untilNewline(rest)])
		}
	}
	if c := rest[0]; strings.IndexByte(l.quotes, c) >= 0 {
		return lx.emit(String, rest[:quoted(rest)])
	}

	r, size := utf8.DecodeRuneInString(rest)
	switch {
	case r == '\n':
		lx.line++
		return 1
	case isDigit(r) && !afterWord(s, i):
		n := scanNumber(rest)
		return lx.emit(Number, rest[:n])
	case isWordStart(r):
		n := scanIdent(rest)
		return lx.emit(l.classify(rest[:n], rest[n:]), rest[:n])
	}
	return lx.emit(Plain, rest[:size])
}

// emit appends text as a span of kind to the current line, splitting it at
// newlines (a block comment or raw string may cover several lines) and
// merging it into the previous span when that one is of the same kind.
// It returns len(text), so callers can emit and advance in one step.
func (lx *lexer) emit(kind Kind, text string) int {
	n := len(text)
	for {
		part, after, more := strings.Cut(text, "\n")
		lx.add(kind, part)
		if !more {
			return n
		}
		lx.line++
		text = after
	}
}

func (lx *lexer) add(kind Kind, text string) {
	if text == "" || lx.line >= len(lx.out) {
		return
	}
	spans := lx.out[lx.line]
	if k := len(spans) - 1; k >= 0 && spans[k].Kind == kind {
		spans[k].Text += text
		return
	}
	lx.out[lx.line] = append(spans, Span{kind, text})
}

// untilNewline returns the length of s up to its first newline, or all of s.
func untilNewline(s string) int {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return i
	}
	return len(s)
}

// until returns the length of s up to and including the first end found
// at or after from, or all of s when it is never closed: a preview shows a
// file's head, and an unclosed comment simply runs to the end of it.
func until(s string, from int, end string) int {
	if i := strings.Index(s[from:], end); i >= 0 {
		return from + i + len(end)
	}
	return len(s)
}

// quoted returns the length of the string literal at the start of s: up to
// the matching quote, skipping backslash escapes. A string never runs past
// the end of its line, so one stray quote cannot colour the rest of a file.
func quoted(s string) int {
	q := s[0]
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case q:
			return i + 1
		case '\n':
			return i
		}
	}
	return len(s)
}

// scanIdent returns the length of the identifier at the start of s. It
// stops at ".", so "fmt.Println" is two words and Println is seen as a call.
func scanIdent(s string) int {
	for i, r := range s {
		if !isWordStart(r) && !isDigit(r) {
			return i
		}
	}
	return len(s)
}

// scanNumber returns the length of the number at the start of s: digits,
// letters, "_" and ".", which covers 42, 0x1F, 1_000, 3.14 and 1e9 without a
// number grammar per language. Precision is not worth more here.
func scanNumber(s string) int {
	for i, r := range s {
		if !isWordStart(r) && !isDigit(r) && r != '.' {
			return i
		}
	}
	return len(s)
}

// afterWord reports whether s[i] continues a word, as the 2 in "utf8" does.
func afterWord(s string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return i > 0 && (isWordStart(r) || isDigit(r))
}

func isWordStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func isDigit(r rune) bool     { return '0' <= r && r <= '9' }
