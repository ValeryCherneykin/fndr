package filter

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// tree makes an index of files and every folder above them, below under,
// as a crawl would.
func tree(files ...string) []indexer.Entry {
	var es []indexer.Entry
	seen := map[string]bool{}
	for _, f := range files {
		parts := strings.Split(f, "/")
		for i := 1; i < len(parts); i++ {
			if d := strings.Join(parts[:i], "/"); !seen[d] {
				seen[d] = true
				es = append(es, dir(d))
			}
		}
		es = append(es, file(f, 10))
	}
	return es
}

// kept returns the paths below under, relative, that tags keep in es.
func kept(t *testing.T, es []indexer.Entry, under string, tags ...string) []string {
	t.Helper()
	var s Stack
	for _, text := range tags {
		tag, err := Parse(text)
		if err != nil {
			t.Fatalf("Parse(%q): %v", text, err)
		}
		s = s.Toggle(tag)
	}
	keep := s.Keep(es, under)
	got := []string{}
	for _, e := range es {
		if below(e.Path, under) && keep(e) {
			got = append(got, strings.TrimPrefix(e.Path, under+"/"))
		}
	}
	slices.Sort(got)
	return got
}

// TestHas: folders found by what they hold, the deepest that holds it all.
func TestHas(t *testing.T) {
	es := tree(
		"dev/fndr/cmd/fndr/main.go",
		"dev/fndr/internal/tui/view.go",
		"dev/fndr/assets/demo.gif",
		"dev/test1/package.json",
		"dev/test2/package.json",
		"dev/test2/Dockerfile",
		"dev/docker/notes.txt",
		"dev/site/web/app.go",
		"dev/site/web/img/logo.gif",
		"dev/site/node_modules/x/index.js",
	)
	for _, tc := range []struct {
		tags  []string
		under string
		want  []string
	}{
		// The owner's: code in cmd/, a demo in assets/: the project, not
		// cmd/, not assets/, not dev/ above it. A second project holding
		// both is found too, at its own depth.
		{[]string{"has:.go", "has:.gif"}, under, []string{"dev/fndr", "dev/site/web"}},
		// "test1 or test2? the one with the json and the Dockerfile."
		{[]string{"has:.json", "has:dockerfile"}, under, []string{"dev/test2"}},
		// One has: finds the folders right around the files.
		{[]string{"has:.go"}, under, []string{"dev/fndr/cmd/fndr", "dev/fndr/internal/tui", "dev/site/web"}},
		// By name: docker/ holds nothing named docker.
		{[]string{"has:docker"}, under, []string{"dev/test2"}},
		// Folders count as held: the project with node_modules.
		{[]string{"has:node_modules"}, under, []string{"dev/site"}},
		{[]string{"has:.json|.js"}, under, []string{"dev/site/node_modules/x", "dev/test1", "dev/test2"}},
		// With a plain tag: the folders also have to pass it.
		{[]string{"has:.go", "site"}, under, []string{"dev/site/web"}},
		{[]string{"has:.go", "files"}, under, []string{}},
		// Only folders below the one searched: fndr itself is not.
		{[]string{"has:.go", "has:.gif"}, under + "/dev/fndr", []string{}},
		{[]string{"has:.go"}, under + "/dev/fndr", []string{"cmd/fndr", "internal/tui"}},
		{[]string{"has:.rs"}, under, []string{}},
	} {
		if got := kept(t, es, tc.under, tc.tags...); !slices.Equal(got, tc.want) {
			t.Errorf("%q under %s: %q, want %q", tc.tags, tc.under, got, tc.want)
		}
	}
}

// TestHasMatchesNaive: on random trees, in random order, holders finds
// what the definition says, checked the slow way: every folder against
// every entry.
func TestHasMatchesNaive(t *testing.T) {
	exts := []string{".go", ".gif", ".md", ".txt"}
	for seed := range uint64(300) {
		r := rand.New(rand.NewPCG(seed, 1)) //nolint:gosec // seeded on purpose: a failure names its seed
		var files []string
		for range 1 + r.IntN(30) {
			path := ""
			for range r.IntN(5) {
				path += fmt.Sprintf("d%d/", r.IntN(3))
			}
			files = append(files, path+fmt.Sprintf("f%d%s", r.IntN(3), exts[r.IntN(len(exts))]))
		}
		es := tree(files...)
		r.Shuffle(len(es), func(i, j int) { es[i], es[j] = es[j], es[i] })
		tags := []string{"has:" + exts[r.IntN(len(exts))], "has:" + exts[r.IntN(len(exts))]}
		if tags[0] == tags[1] {
			tags = tags[:1]
		}
		if got, want := kept(t, es, under, tags...), naiveHas(es, tags); !slices.Equal(got, want) {
			t.Fatalf("seed %d, %q over %q:\n got %q\nwant %q", seed, tags, files, got, want)
		}
	}
}

func naiveHas(es []indexer.Entry, tags []string) []string {
	holds := func(d string) bool {
		for _, text := range tags {
			ext := strings.TrimPrefix(text, "has:")
			if !slices.ContainsFunc(es, func(e indexer.Entry) bool {
				return strings.HasPrefix(e.Path, d+"/") && strings.HasSuffix(e.Name(), ext)
			}) {
				return false
			}
		}
		return true
	}
	got := []string{}
	for _, e := range es {
		if !e.IsDir || !holds(e.Path) {
			continue
		}
		if !slices.ContainsFunc(es, func(c indexer.Entry) bool {
			return c.IsDir && parent(c.Path) == e.Path && holds(c.Path)
		}) {
			got = append(got, strings.TrimPrefix(e.Path, under+"/"))
		}
	}
	slices.Sort(got)
	return got
}

// BenchmarkHas: what two has: tags add to a search over 100k entries, the
// pass that finds the folders, done again for every key typed.
func BenchmarkHas(b *testing.B) {
	es := benchIndex()
	var s Stack
	for _, text := range []string{"has:.go", "has:.md"} {
		tag, err := Parse(text)
		if err != nil {
			b.Fatal(err)
		}
		s = s.Toggle(tag)
	}
	b.ReportAllocs()
	for b.Loop() {
		s.Keep(es, under)
	}
}
