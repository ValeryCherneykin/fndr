package indexer

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// pruneVersion is bumped whenever the rules by name change (junkNames,
// bundleExts, how .gitignore is read): an index saved under the old ones
// may lack what they no longer leave out.
const pruneVersion = 2

// Prune says which folders a crawl lists but does not go into. On the
// author's home, caches, package stores and app internals were 80% of the
// entries and 85% of the crawl time, and they crowded search: nobody
// looks for a file inside ~/Library/Caches or node_modules by name.
//
// A pruned folder is still an entry, so search finds it by name and the
// user can go in. Only its contents stay out of the index. The crawl's
// root is never pruned: fndr opened inside ~/.config indexes it, as asked.
//
// A nil *Prune prunes nothing. .git is a separate, harder rule of Crawl:
// it is left out altogether.
type Prune struct {
	paths     map[string]bool // absolute folders, such as ~/Library
	keep      map[string]bool // absolute hidden folders gone into all the same: ~/.config
	gitignore bool            // folders that .gitignore files name, too (gitignore.go)
}

// junkNames are folders that tools fill, wherever they are.
var junkNames = map[string]bool{
	"node_modules": true,
	"__pycache__":  true,
	// Browsers' and editors' caches, which on Linux sit in ~/.config
	// (google-chrome/Default/Cache, Code/CachedData): thousands of files
	// next to the settings fndr goes into ~/.config for.
	"Cache":      true,
	"Code Cache": true,
	"GPUCache":   true,
	"CachedData": true,
}

// bundleExts are macOS bundles: folders that the Finder shows, and the
// user thinks of, as one file. Ghostty.app is found and opened; what is in
// Contents/ is the app's business.
var bundleExts = map[string]bool{
	".app":           true,
	".bundle":        true,
	".framework":     true,
	".plugin":        true,
	".kext":          true,
	".photoslibrary": true,
	".musiclibrary":  true,
	".tvlibrary":     true,
	".logicx":        true,
	".band":          true,
	".xcarchive":     true,
}

// DefaultPrune is what fndr leaves out by default, for the user whose home
// is home: hidden folders, the folders in junkNames and bundleExts, the
// folders .gitignore files name, the macOS ~/Library (application data,
// two thirds of it caches) and the Go package store ($GOPATH/pkg, and
// GOMODCACHE if moved elsewhere).
//
// Hidden but gone into: ~/.config, or $XDG_CONFIG_HOME. It holds the
// user's own settings, nvim/init.lua, ghostty/config, a few dozen files
// looked for by name all the time, not a tool's dump.
func DefaultPrune(home string) *Prune {
	p := &Prune{paths: map[string]bool{}, keep: map[string]bool{}, gitignore: true}
	if conf := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(conf) {
		p.keep[filepath.Clean(conf)] = true
	}
	if home == "" {
		return p // no home to find ~/Library under: the rules by name still hold
	}
	p.paths[filepath.Join(home, "Library")] = true
	if len(p.keep) == 0 {
		p.keep[filepath.Join(home, ".config")] = true
	}

	// GOPATH may list several folders; go install, and so the package
	// store, uses the first.
	gopath := filepath.Join(home, "go")
	if list := filepath.SplitList(os.Getenv("GOPATH")); len(list) > 0 && filepath.IsAbs(list[0]) {
		gopath = list[0]
	}
	p.paths[filepath.Join(gopath, "pkg")] = true
	if mc := os.Getenv("GOMODCACHE"); filepath.IsAbs(mc) {
		p.paths[filepath.Clean(mc)] = true
	}
	return p
}

// Rules describes what p leaves out, so a saved index can tell whether it
// was crawled under the rules of now (see Saved.Rules).
func (p *Prune) Rules() string {
	if p == nil {
		return "none"
	}
	return fmt.Sprintf("v%d gitignore=%t %q keep %q", pruneVersion, p.gitignore,
		slices.Sorted(maps.Keys(p.paths)), slices.Sorted(maps.Keys(p.keep)))
}

// Reaches reports whether a crawl from root goes into dir, at or below it:
// not if a folder on the way, dir included, is one the crawl lists without
// entering. The interface asks it when the user goes into a folder, to
// crawl it if the index has nothing of what is inside.
func (p *Prune) Reaches(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if p == nil || err != nil || rel == "." {
		return true
	}
	stack := p.ignoresIn(nil, root)
	at := root
	for name := range strings.SplitSeq(rel, string(filepath.Separator)) {
		at = filepath.Join(at, name)
		if p.Prunes(at, name) || stack.ignores(at) {
			return false
		}
		stack = p.ignoresIn(stack, at)
	}
	return true
}

// ignoresIn returns the .gitignore files that apply below dir: those of
// its parents, in stack, and its own.
func (p *Prune) ignoresIn(stack ignoreStack, dir string) ignoreStack {
	if p == nil || !p.gitignore {
		return nil
	}
	return stack.with(loadGitignore(dir))
}

// Prunes reports whether a crawl lists the folder at path, named name,
// without going in. The .gitignore files are the crawl's to apply (see
// Reaches): they hang on the folders above, which a name does not tell.
func (p *Prune) Prunes(path, name string) bool {
	if p == nil || p.keep[path] {
		return false
	}
	return strings.HasPrefix(name, ".") ||
		junkNames[name] ||
		bundleExts[strings.ToLower(filepath.Ext(name))] ||
		p.paths[path]
}
