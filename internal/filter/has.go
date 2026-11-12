package filter

import (
	"path/filepath"
	"strings"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// A has: tag finds folders by what they hold: has:.json has:dockerfile is
// the folder the user remembers by its files, not by its name. What
// follows has: is any tag, matched against names: has:docker is a folder
// holding something named docker, not every folder below a docker/.
//
// What a folder holds is known only from the entries below it, so a has:
// tag cannot be tested on one entry alone: Keep goes over the snapshot
// once first (holders), and the search then tests folders against what
// it found.

// hasPrefix starts a has: tag.
const hasPrefix = "has:"

// maxHas is how many has: tags a search can take: one bit each. Nobody
// types 65; past that a search finds nothing rather than a wrong answer.
const maxHas = 64

// Keep returns the test a search runs on each entry below under: every
// tag of s must match. With has: tags it first goes over snap to find the
// folders that hold what they ask for, a pass over the whole index: call
// it where the search runs, off the UI goroutine.
func (s Stack) Keep(snap []indexer.Entry, under string) func(indexer.Entry) bool {
	var plain Stack
	var has []Tag
	for _, t := range s {
		if t.has {
			has = append(has, t)
		} else {
			plain = append(plain, t)
		}
	}
	if len(has) == 0 {
		return func(e indexer.Entry) bool { return plain.match(e, under) }
	}
	if len(has) > maxHas {
		return func(indexer.Entry) bool { return false }
	}
	found := holders(snap, under, has)
	return func(e indexer.Entry) bool {
		return found[e.Path] && plain.match(e, under) // found holds folders only
	}
}

// holders returns the folders below under that hold, anywhere inside,
// something for each of has, and hold no such folder themselves: the
// deepest. has:.go has:.gif finds the project with code in cmd/ and a
// demo in assets/, but not cmd/ (no .gif), and not the folders above the
// project (it is in them).
//
// Each entry that matches a has: tag climbs its folders and sets that
// tag's bit on each. The climb stops at a folder that has the bits
// already: whoever set them climbed on from there, so every folder above
// has them too. Each folder is so climbed through once per tag at most,
// and a pass costs about one map lookup per matching entry.
//
// Then a folder with every bit set is a holder; its parent, which has
// every bit too, is not the deepest one. Map keys are slices of the
// entries' paths: no path is copied.
func holders(snap []indexer.Entry, under string, has []Tag) map[string]bool {
	all := uint64(1)<<len(has) - 1 // 1<<64 is 0 in Go: all 64 bits then
	marks := make(map[string]uint64)
	for i := range snap {
		e := &snap[i]
		if !below(e.Path, under) {
			continue // no folder above it can be a result; this only saves the climb
		}
		// Matched under its own folder, a piece of text is looked for in
		// the name alone. Not a branch in Tag.Match: that one runs on every
		// entry of every search, and the check cost 10% there.
		in := parent(e.Path)
		var bits uint64
		for j := range has {
			if has[j].Match(*e, in) {
				bits |= 1 << j
			}
		}
		if bits == 0 {
			continue
		}
		for d := in; len(d) > len(under); d = parent(d) { // under itself is never a result
			old := marks[d]
			if old|bits == old {
				break
			}
			marks[d] = old | bits
		}
	}

	found := make(map[string]bool)
	for d, bits := range marks {
		if bits == all {
			found[d] = true
		}
	}
	for d := range found {
		if p := parent(d); found[p] {
			found[p] = false // not the deepest: d is inside it
		}
	}
	return found
}

// below reports whether path lies strictly below dir; every path is below "".
func below(path, dir string) bool {
	if dir == "" {
		return true
	}
	return len(path) > len(dir) && strings.HasPrefix(path, dir) &&
		(dir[len(dir)-1] == filepath.Separator || path[len(dir)] == filepath.Separator)
}

// parent returns the folder path is in, as a slice of it: "" for a path
// right under the root, which is below nothing worth holding.
func parent(path string) string {
	return path[:max(strings.LastIndexByte(path, filepath.Separator), 0)]
}
