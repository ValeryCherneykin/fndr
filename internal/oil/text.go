package oil

import (
	"unicode"
	"unicode/utf8"
)

// The cursor moves by characters, not bytes and not runes. A character
// here is a rune with the combining marks that follow it: macOS often
// stores names decomposed (NFD), so "й" is "и" and U+0306, two runes the
// user sees as one letter. Stepping by runes, l would stop between them
// and x would erase the mark alone, leaving "и". Vim treats them as one
// character, and so does the buffer.
//
// Emoji joined into one picture (ZWJ sequences) are still several
// characters to it; names with them are rare enough not to need the full
// Unicode grapheme rules, which the standard library does not have.

// nextChar returns the start of the character after the one at i, or
// len(s) at the end.
func nextChar(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	_, size := utf8.DecodeRuneInString(s[i:])
	for i += size; i < len(s); i += size {
		var r rune
		r, size = utf8.DecodeRuneInString(s[i:])
		if !unicode.Is(unicode.Mn, r) {
			break
		}
	}
	return i
}

// prevChar returns the start of the character before i, or 0 at the start.
func prevChar(s string, i int) int {
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		i -= size
		if !unicode.Is(unicode.Mn, r) {
			break
		}
	}
	return i
}

// lastChar returns the start of the last character of s: the rightmost
// place the cursor can be in normal mode. It is 0 for an empty line.
func lastChar(s string) int { return prevChar(s, len(s)) }

// charIndex returns how many characters come before the byte offset col.
func charIndex(s string, col int) int {
	n := 0
	for i := 0; i < col; i = nextChar(s, i) {
		n++
	}
	return n
}

// colOf returns the byte offset of character number idx, or of the last
// character if the line is shorter.
func colOf(s string, idx int) int {
	i := 0
	for ; idx > 0 && i < len(s); idx-- {
		i = nextChar(s, i)
	}
	return min(i, lastChar(s))
}

// firstNonBlank returns the start of the first character that is not a
// space or a tab, where ^ goes and where line commands leave the cursor.
// It steps by characters: a blank with a combining mark on it is one
// character, and stepping by bytes would stop inside it.
func firstNonBlank(s string) int {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i = nextChar(s, i)
	}
	return min(i, lastChar(s))
}
