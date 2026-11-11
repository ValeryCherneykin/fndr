// Package filter turns tags into tests on index entries. A tag narrows a
// search the way a card on a desk does: .go, dirs, size>1MB, image. Tags
// stack: an entry must pass every one (AND), and a single tag may offer
// choices separated by "|", of which one is enough (OR): .go|dirs.
//
// A tag is kept as data, not as a closure: it has to be drawn as a chip,
// compared with another (adding the same tag again removes it), and
// tested. The search gets a plain func at its border (Stack.Match as a
// method value), since the indexer cannot know this package.
//
// Matching runs on every entry for every key typed, so it never allocates.
package filter

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// ErrEmpty is returned by Parse for a tag with nothing in it, or a choice
// left empty: "", " ", ".go|".
var ErrEmpty = errors.New("empty tag")

// Tag is one parsed tag: a list of choices, of which an entry must match
// at least one.
type Tag struct {
	terms []term
	text  string // the tag as Parse cleaned it up, see String
}

type kind uint8

const (
	kindText     kind = iota // a piece of the path below the search root
	kindExt                  // the name ends in .ext: .go, .tar.gz
	kindCategory             // a family of extensions: image, code
	kindDirs                 // folders only
	kindFiles                // everything but folders
	kindLarger               // a file larger than n bytes
	kindSmaller              // a file smaller than n bytes
)

type term struct {
	kind kind
	text string   // lower case: the piece of path, or the extension without its dot
	cat  category // for kindCategory: every category of the tag, see Parse
	n    int64    // bytes, for kindLarger and kindSmaller
}

// Parse reads a tag as typed after ctrl+t, case-insensitively. Each choice
// is, in this order:
//
//   - dirs or files;
//   - a category: audio, image, video, doc, code, archive;
//   - a size: size>1MB, size<10k, or short >1mb; units B, K(B), M(B),
//     G(B), T(B) in powers of 1000, as sizes are shown;
//   - an extension: .go, .tar.gz;
//   - anything else: a piece of the path below the folder searched.
func Parse(s string) (Tag, error) {
	parts := strings.Split(s, "|")
	t := Tag{terms: make([]term, 0, len(parts))}
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			return Tag{}, ErrEmpty
		}
		tm, text, err := parseTerm(p)
		if err != nil {
			return Tag{}, err
		}
		texts = append(texts, text)
		// All categories of a tag go into one choice: the extension is
		// then looked up once per entry, not once per category.
		if i := slices.IndexFunc(t.terms, func(o term) bool { return o.kind == kindCategory }); i >= 0 && tm.kind == kindCategory {
			t.terms[i].cat |= tm.cat
			continue
		}
		t.terms = append(t.terms, tm)
	}
	t.text = strings.Join(texts, "|")
	return t, nil
}

// parseTerm reads one choice, already trimmed and in lower case, and
// returns it with the text it is shown as.
func parseTerm(p string) (term, string, error) {
	switch p {
	case "dirs":
		return term{kind: kindDirs}, p, nil
	case "files":
		return term{kind: kindFiles}, p, nil
	}
	if c, ok := categories[p]; ok {
		return term{kind: kindCategory, cat: c}, p, nil
	}
	if size, ok := strings.CutPrefix(p, "size"); ok && size != "" && (size[0] == '>' || size[0] == '<') {
		p = size // size>1mb is >1mb with a word for those who read it
	}
	if p[0] == '>' || p[0] == '<' {
		n, err := parseSize(p[1:])
		if err != nil {
			return term{}, "", fmt.Errorf("size %q: %w", p[1:], err)
		}
		k := kindLarger
		if p[0] == '<' {
			k = kindSmaller
		}
		return term{kind: k, n: n}, "size" + p, nil
	}
	if ext, ok := strings.CutPrefix(p, "."); ok && ext != "" && !strings.Contains(ext, "/") {
		return term{kind: kindExt, text: ext}, p, nil
	}
	return term{kind: kindText, text: p}, p, nil
}

// parseSize reads 1mb, 1.5MB, 300k, 42: a number and a unit in powers of
// 1000, bytes if none.
func parseSize(s string) (int64, error) {
	num := strings.TrimRight(s, "kmgtb")
	mult := 1.0
	switch strings.TrimSuffix(s[len(num):], "b") {
	case "":
	case "k":
		mult = 1e3
	case "m":
		mult = 1e6
	case "g":
		mult = 1e9
	case "t":
		mult = 1e12
	default:
		return 0, errors.New("unit is one of B, KB, MB, GB, TB")
	}
	// Digits and one dot: ParseFloat would also take nan, inf, 1e9, 0x1p4.
	if num == "" || strings.Trim(num, "0123456789.") != "" || strings.Count(num, ".") > 1 || num == "." {
		return 0, errors.New("not a size")
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f*mult > 1<<62 {
		return 0, errors.New("not a size")
	}
	return int64(f * mult), nil
}

// String returns the tag as it is drawn and compared: lower case, without
// spaces around the choices, a size always with its word (size>1mb).
func (t Tag) String() string { return t.text }

// Match reports whether e matches one of t's choices. under is the folder
// searched: a piece of text is looked for in the path below it, since
// the folders above are the same for every entry.
func (t Tag) Match(e indexer.Entry, under string) bool {
	for i := range t.terms {
		if t.terms[i].match(e, under) {
			return true
		}
	}
	return false
}

func (tm *term) match(e indexer.Entry, under string) bool {
	switch tm.kind {
	case kindDirs:
		return e.IsDir
	case kindFiles:
		return !e.IsDir
	case kindLarger:
		return !e.IsDir && e.Size > tm.n
	case kindSmaller:
		return !e.IsDir && e.Size < tm.n
	case kindExt:
		name := e.Name()
		return len(name) > len(tm.text)+1 && name[len(name)-len(tm.text)-1] == '.' &&
			strings.EqualFold(name[len(name)-len(tm.text):], tm.text)
	case kindCategory:
		return !e.IsDir && categoryOf(e.Name())&tm.cat != 0
	default:
		rel := e.Path
		if under != "" && len(rel) > len(under) && strings.HasPrefix(rel, under) {
			rel = rel[len(under):]
		}
		return containsFold(rel, tm.text)
	}
}

// containsFold reports whether lower, already in lower case, is in s in
// any case, without making a lower-case copy of s.
//
// A full EqualFold at every position of every path was most of a tag's
// cost: when the piece starts with an ASCII byte, a position is first
// tried on that byte alone, as strings.Index does, which rules out
// nearly all of them. The window is as long in bytes as the piece, so
// letters that fold into one of another length (the Kelvin sign into k)
// never matched anyway: the skip loses nothing.
func containsFold(s, lower string) bool {
	if lower == "" {
		return true
	}
	c0 := lower[0]
	up := c0
	if 'a' <= c0 && c0 <= 'z' {
		up -= 'a' - 'A'
	}
	ascii := c0 < 0x80
	for i := 0; i+len(lower) <= len(s); i++ {
		if ascii && s[i] != c0 && s[i] != up {
			continue
		}
		if strings.EqualFold(s[i:i+len(lower)], lower) {
			return true
		}
	}
	return false
}
