package indexer

import (
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// The scoring and matching as they were before the fast paths (a query
// decoded once, a lower-case table): kept as the reference the fast code
// must agree with, bit for bit (TestScoreMatchesRef, FuzzScoreMatchesRef).

func refFold(r rune) rune {
	if r < utf8.RuneSelf {
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		return r
	}
	return unicode.ToLower(r)
}

func fuzzyScoreRef(e *Entry, from int, lowerQuery string, pos []int) (score int, ok bool) {
	if lowerQuery == "" {
		return 0, true
	}
	s := e.Path

	// Pass 1: find the latest start.
	start := -1
	qi := len(lowerQuery) // lowerQuery[:qi] is still unmatched
	for i := len(s); i > from && qi > 0; {
		r, size := lastRune(s[:i])
		i -= size // i is now the start of r
		qr, qsize := lastRune(lowerQuery[:qi])
		if refFold(r) == qr {
			qi -= qsize
			start = i
		}
	}
	if qi > 0 {
		return 0, false
	}

	// Pass 2: compact match from start, scoring as we go. Pass 1 proved that
	// the whole query fits in s[start:], so this loop always completes.
	nameOff := int(e.nameOff)
	prevEnd := -1 // byte offset just past the previous match, -1 if none
	k := 0        // next free slot in pos
	for i := start; qi < len(lowerQuery); {
		r, size := firstRune(s[i:])
		qr, qsize := firstRune(lowerQuery[qi:])
		if refFold(r) == qr {
			qi += qsize

			score += scoreMatch + positionBonus(s, i, r)
			if i >= nameOff {
				score += bonusName
			}
			if prevEnd >= 0 {
				if gap := i - prevEnd; gap == 0 {
					score += bonusConsecutive
				} else {
					score += penaltyGapStart + (gap-1)*penaltyGapExtend
				}
			}
			prevEnd = i + size

			if pos != nil {
				pos[k] = i
				k++
			}
		}
		i += size
	}
	return score, true
}

func hasSubsequenceFoldRef(s, lowerQuery string) bool {
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
		if unicode.ToLower(r) == qr {
			qi += qsize
			if qi == len(lowerQuery) {
				return true
			}
		}
		i += size
	}
	return false
}

// lastRune decodes the last rune of s, with an ASCII fast path.
func lastRune(s string) (r rune, size int) {
	if c := s[len(s)-1]; c < utf8.RuneSelf {
		return rune(c), 1
	}
	return utf8.DecodeLastRuneInString(s)
}

// TestFoldTable: the table is unicode.ToLower, rune for rune.
func TestFoldTable(t *testing.T) {
	for r := range rune(len(lowerTable)) {
		if got, want := foldRune(r), unicode.ToLower(r); got != want {
			t.Fatalf("%U: %U, want %U", r, got, want)
		}
	}
}

// FuzzScoreMatchesRef: the fast scoring and matching agree with the
// reference bit for bit: match or not, score, highlighted positions.
func FuzzScoreMatchesRef(f *testing.F) {
	for _, s := range [][2]string{
		{"/home/me/dev/fndr/internal/tui/main.go", "main"},
		{"/home/me/Документы/Микросервисы, как в BigTech/README.md", "микро"},
		{"/home/me/\u212Aafka/x", "kaf"}, // the Kelvin sign lowers to k
		{"/home/me/\u0130stanbul.txt", "ist"},
		{"/home/me/FooBar/fooBAR.go", "fb"},
		{"/home/me/bad\xffname/x.go", "bx"},
		{"/a", ""},
	} {
		f.Add(s[0], s[1], 0)
	}
	f.Fuzz(func(t *testing.T, path, query string, from int) {
		e := NewEntry(path, 0, 0, false)
		from = max(min(from, len(path)), 0)
		lq := strings.ToLower(query)
		n := utf8.RuneCountInString(lq)
		gotPos, wantPos := make([]int, n), make([]int, n)
		got, gotOK := scoreRunes(&e, from, []rune(lq), gotPos)
		want, wantOK := fuzzyScoreRef(&e, from, lq, wantPos)
		if got != want || gotOK != wantOK || gotOK && !slices.Equal(gotPos, wantPos) {
			t.Fatalf("%q in %q from %d: %d %v %v, want %d %v %v", lq, path, from, got, gotOK, gotPos, want, wantOK, wantPos)
		}
		if a, b := hasSubsequenceFold(path, lq), hasSubsequenceFoldRef(path, lq); a != b {
			t.Fatalf("%q in %q: matches %v, want %v", lq, path, a, b)
		}
	})
}
