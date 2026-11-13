package indexer

import (
	"unicode/utf8"
)

// hasSubsequenceFold reports whether the characters of lowerQuery appear in s
// in order, not necessarily next to each other, ignoring case: "mngo"
// matches "Main.go". This is the first, cheap stage of fuzzy search. It runs
// on every entry for every keystroke and only answers "can this match at
// all?"; scoring the survivors is a separate, more expensive stage.
//
// lowerQuery must already be lowercased with strings.ToLower. The caller does
// that once per keystroke, so the per-entry work below never allocates.
//
// The algorithm is a single left-to-right scan with one cursor into the
// query: each character of s that equals the character under the cursor
// advances it, and the query matches once the cursor reaches its end.
//
// Case folding is per rune with unicode.ToLower, the same mapping
// strings.ToLower uses, so both sides agree. It is simple folding: "ё" does
// not match "е", and "ß" does not match "ss".
func hasSubsequenceFold(s, lowerQuery string) bool {
	if lowerQuery == "" {
		return true
	}

	qi := 0 // byte offset in lowerQuery of the character we are looking for
	for i := 0; i < len(s); {
		c := s[i]

		// ASCII fast path. Most paths are pure ASCII, and for them a byte
		// comparison is enough: uppercase and lowercase ASCII letters differ
		// by exactly 'a'-'A' (32). If the query character at qi is not ASCII,
		// its first byte is >= utf8.RuneSelf and can never equal c, which is
		// the correct answer.
		if c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c == lowerQuery[qi] {
				qi++
				if qi == len(lowerQuery) {
					return true
				}
			}
			i++
			continue
		}

		// Multi-byte UTF-8, such as Cyrillic: decode whole runes on both
		// sides. Still no allocation, just a few more instructions.
		r, size := utf8.DecodeRuneInString(s[i:])
		qr, qsize := utf8.DecodeRuneInString(lowerQuery[qi:])
		if foldRune(r) == qr {
			qi += qsize
			if qi == len(lowerQuery) {
				return true
			}
		}
		i += size
	}
	return false
}
