package filter

import (
	"slices"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// Stack is the tags of a search, in the order they were added: an entry
// must match every one. The empty Stack matches everything.
//
// A Stack is never changed once made: a search in flight on another
// goroutine may be reading it. Toggle returns a new one.
type Stack []Tag

// Toggle returns s with t added, or, if s holds it already, without it:
// adding a tag again takes it off, the way a card is picked up again.
func (s Stack) Toggle(t Tag) Stack {
	if i := slices.IndexFunc(s, func(o Tag) bool { return o.text == t.text }); i >= 0 {
		return slices.Delete(slices.Clone(s), i, i+1)
	}
	return append(slices.Clip(s), t)
}

// Match reports whether e matches every tag of s. under is the folder
// searched, see Tag.Match.
func (s Stack) Match(e indexer.Entry, under string) bool {
	for i := range s {
		if !s[i].Match(e, under) {
			return false
		}
	}
	return true
}
