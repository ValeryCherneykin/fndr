package indexer

import (
	"path/filepath"
	"unicode"
	"unicode/utf8"
)

// Score weights for fuzzyScore, in the spirit of fzf. Only their relative
// sizes matter. Each matched character earns scoreMatch plus at most one
// position bonus; gaps between matched characters cost points, so compact
// matches beat scattered ones.
const (
	scoreMatch = 16

	// Position bonuses: where a matched character sits in the path.
	bonusSegment   = 10 // first character of a path segment: after "/" or at the start
	bonusDelimiter = 8  // after "_", "-", "." or " ": the start of a word in a name
	bonusCamel     = 7  // an uppercase letter after a lowercase one: "B" in "fooBar"

	// bonusConsecutive rewards a character that directly follows the
	// previous matched one: "main" typed and found as "main".
	bonusConsecutive = 6

	// bonusName rewards characters matched inside the base name. Users type
	// file names, not directory names, so /p/main.go must beat /main/p.go.
	bonusName = 4

	// Gap penalties, per gap between two matched characters: a fixed cost for
	// opening the gap plus a small cost per extra skipped byte.
	penaltyGapStart  = -3
	penaltyGapExtend = -1
)

// fuzzyScore scores how well lowerQuery matches e.Path as a case-insensitive
// subsequence; ok is false if it does not match at all. It is the second
// search stage, run only on entries that passed hasSubsequenceFold.
//
// If pos is non-nil it receives the byte offsets in e.Path of the matched
// characters, in ascending order, for highlighting. It must have room for
// one offset per rune of lowerQuery. Search passes nil while ranking every
// candidate and asks for positions only for the rows on screen, so the hot
// path never allocates.
//
// Matching is a two-pass scan, the trick fzf uses for its fast mode:
//
//  1. Right to left, match each query character as late as possible. This
//     finds the latest position where a match can start. In a path the most
//     specific part, the file name, is at the end, so starting late prefers
//     the name over directories.
//  2. Left to right from that start, match each character as early as
//     possible. This makes the match compact: for "go" in "/a/go.mod", pass
//     one alone would take the "o" of "mod"; pass two takes "go".
//
// Both passes are O(len(path)) and allocation-free. The result is a very good
// match, not always the best one: fzf's optimal algorithm costs
// O(len(path) * len(query)) per entry, which is not worth it for paths.
func fuzzyScore(e *Entry, lowerQuery string, pos []int) (score int, ok bool) {
	if lowerQuery == "" {
		return 0, true
	}
	s := e.Path

	// Pass 1: find the latest start.
	start := -1
	qi := len(lowerQuery) // lowerQuery[:qi] is still unmatched
	for i := len(s); i > 0 && qi > 0; {
		r, size := lastRune(s[:i])
		i -= size // i is now the start of r
		qr, qsize := lastRune(lowerQuery[:qi])
		if foldRune(r) == qr {
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
		if foldRune(r) == qr {
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

// positionBonus scores where the rune r starting at byte offset i of s sits.
func positionBonus(s string, i int, r rune) int {
	if i == 0 {
		return bonusSegment
	}
	switch prev := s[i-1]; {
	case prev == filepath.Separator:
		return bonusSegment
	case prev == '_' || prev == '-' || prev == '.' || prev == ' ':
		return bonusDelimiter
	case 'a' <= prev && prev <= 'z' && 'A' <= r && r <= 'Z':
		return bonusCamel
	}
	return 0
}

// firstRune decodes the first rune of s, with an ASCII fast path.
func firstRune(s string) (r rune, size int) {
	if c := s[0]; c < utf8.RuneSelf {
		return rune(c), 1
	}
	return utf8.DecodeRuneInString(s)
}

// lastRune decodes the last rune of s, with an ASCII fast path.
func lastRune(s string) (r rune, size int) {
	if c := s[len(s)-1]; c < utf8.RuneSelf {
		return rune(c), 1
	}
	return utf8.DecodeLastRuneInString(s)
}

// foldRune lowercases r the way strings.ToLower does, with an ASCII fast path.
func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		return r
	}
	return unicode.ToLower(r)
}
