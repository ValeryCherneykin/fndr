package indexer

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// A project's .gitignore already names what its tools make: bin/, dist/,
// target/, build/. The crawl reads it to prune those folders, as it prunes
// node_modules: listed, found by name, not entered.
//
// Only folders. Git also ignores files, but those are often exactly what
// the user looks for: .env, a local config, notes kept out of the repo.
// Search hides nothing of that kind; it only skips the trees that tools
// fill. For the same reason only .gitignore files are read, not
// .git/info/exclude or the global excludes, which list personal files and
// editor litter rather than build output.
//
// The patterns follow git's rules, for what a folder can match: "#"
// comments, "!" re-includes, a trailing "/" means folders (all that is
// matched here anyway), a "/" at the start or inside anchors the pattern to
// the .gitignore's folder, else it matches a name at any depth; "*", "?",
// "[a-z]" within a name, "**" across folders. A deeper .gitignore has the
// last word, and within a file the last matching line wins.

// maxGitignore caps how much of a .gitignore is read. Real ones are a few
// kilobytes; a huge file is not a list of patterns, and must not stall the
// crawl.
const maxGitignore = 1 << 20

// gitignore is the patterns of one .gitignore file.
type gitignore struct {
	base string // the folder the file is in: anchored patterns start here
	pats []ignorePattern
}

type ignorePattern struct {
	segs     []string // the pattern split on "/"
	anchored bool     // matched from base; else segs is one name, matched at any depth
	negate   bool     // "!": re-include what an earlier line ignored
}

// loadGitignore reads dir's .gitignore, or returns nil if it has none or
// it cannot be read: a broken ignore file must not stop a crawl.
func loadGitignore(dir string) *gitignore {
	f, err := os.Open(filepath.Join(dir, ".gitignore")) //nolint:gosec // reading the user's own tree is the point
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only: Close has nothing to report
	data, err := io.ReadAll(io.LimitReader(f, maxGitignore))
	if err != nil {
		return nil
	}
	return parseGitignore(dir, data)
}

// parseGitignore reads the patterns of a .gitignore in the folder base.
// It returns nil for a file with no patterns.
func parseGitignore(base string, data []byte) *gitignore {
	g := &gitignore{base: base}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		// Trailing spaces are dropped, unless escaped: "name\ ".
		for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, "\\ ") {
			line = line[:len(line)-1]
		}
		if line == "" || line[0] == '#' {
			continue
		}
		var p ignorePattern
		switch {
		case line[0] == '!':
			p.negate, line = true, line[1:]
		case strings.HasPrefix(line, `\!`), strings.HasPrefix(line, `\#`):
			line = line[1:] // a name that starts with ! or #
		}
		line = strings.TrimSuffix(line, "/") // folders only: all this ever matches
		if strings.Contains(line, "/") {
			p.anchored, line = true, strings.TrimPrefix(line, "/")
		}
		if line == "" {
			continue
		}
		p.segs = strings.Split(line, "/")
		g.pats = append(g.pats, p)
	}
	if len(g.pats) == 0 {
		return nil
	}
	return g
}

// match reports whether a line of g, which may be nil, matches the folder at rel, a path below
// g.base split into names, and if so whether the last such line ignores it
// or re-includes it.
func (g *gitignore) match(rel []string) (matched, ignored bool) {
	if g == nil {
		return false, false
	}
	for i := range g.pats {
		if g.pats[i].matches(rel) {
			matched, ignored = true, !g.pats[i].negate
		}
	}
	return matched, ignored
}

func (p *ignorePattern) matches(rel []string) bool {
	if !p.anchored {
		ok, _ := path.Match(p.segs[0], rel[len(rel)-1]) //nolint:errcheck // a broken pattern matches nothing
		return ok
	}
	return matchSegs(p.segs, rel)
}

// matchSegs matches a path name by name. "**" stands for any number of
// folders, none included, except at the end: "out/**" is what is inside
// out, not out itself.
func matchSegs(pat, rel []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			if len(pat) == 1 {
				return len(rel) > 0
			}
			for i := range len(rel) + 1 {
				if matchSegs(pat[1:], rel[i:]) {
					return true
				}
			}
			return false
		}
		if len(rel) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], rel[0]); !ok { //nolint:errcheck // a broken pattern matches nothing
			return false
		}
		pat, rel = pat[1:], rel[1:]
	}
	return len(rel) == 0
}

// ignoreStack is the .gitignore files that apply in a folder: those of the
// crawl's root and every folder down to it, outermost first.
type ignoreStack []*gitignore

// with returns s with g added, or s if g is nil. It never writes into s:
// sibling folders share the stack of their parent, and one's file must not
// end up in the other's.
func (s ignoreStack) with(g *gitignore) ignoreStack {
	if g == nil {
		return s
	}
	return append(s[:len(s):len(s)], g)
}

// ignores reports whether the folder at path, below every file of s, is
// ignored.
func (s ignoreStack) ignores(dir string) bool {
	ignored := false
	for _, g := range s {
		rel, err := filepath.Rel(g.base, dir)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		if m, ig := g.match(strings.Split(filepath.ToSlash(rel), "/")); m {
			ignored = ig
		}
	}
	return ignored
}
