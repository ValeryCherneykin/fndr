package filter

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

const under = "/home/me"

func file(rel string, size int64) indexer.Entry {
	return indexer.NewEntry(under+"/"+rel, size, 0, false)
}

// dir is a folder of 4096 bytes, as on ext4: a size, but not one to filter by.
func dir(rel string) indexer.Entry { return indexer.NewEntry(under+"/"+rel, 4096, 0, true) }

func TestMatch(t *testing.T) {
	var (
		mainGo  = file("dev/fndr/main.go", 2_000)
		readme  = file("dev/fndr/README.MD", 900)
		photo   = file("Pictures/IMG_1.HEIC", 3_500_000)
		tarball = file("Downloads/x.tar.gz", 50_000_000)
		zshrc   = file(".zshrc", 100)
		goDir   = dir("dev/go")
		fakeGo  = dir("dev/tools.go") // a folder with an extension
		cargo   = file("bin/cargo", 5_000)
		dotPy   = file(".py", 10) // hidden, with no extension
	)
	all := []indexer.Entry{mainGo, readme, photo, tarball, zshrc, goDir, fakeGo, cargo, dotPy}
	for _, tc := range []struct {
		tag  string
		want []indexer.Entry
	}{
		{".go", []indexer.Entry{mainGo, fakeGo}},
		{".GO", []indexer.Entry{mainGo, fakeGo}},
		{".md", []indexer.Entry{readme}},
		{".gz", []indexer.Entry{tarball}},
		{".tar.gz", []indexer.Entry{tarball}},
		{".zshrc", nil}, // the whole name, not an extension
		{"dirs", []indexer.Entry{goDir, fakeGo}},
		{"files", []indexer.Entry{mainGo, readme, photo, tarball, zshrc, cargo, dotPy}},
		{".py", nil},
		{"image", []indexer.Entry{photo}},
		{"code", []indexer.Entry{mainGo}}, // not the folder tools.go
		{"doc", []indexer.Entry{readme}},
		{"archive", []indexer.Entry{tarball}},
		{"size>1MB", []indexer.Entry{photo, tarball}},
		{">1mb", []indexer.Entry{photo, tarball}},
		{"size>3.5mb", []indexer.Entry{tarball}}, // larger, not as large
		{"size<1k", []indexer.Entry{readme, zshrc, dotPy}},
		{"<1000", []indexer.Entry{readme, zshrc, dotPy}}, // folders have no size to speak of
		{">1k", []indexer.Entry{mainGo, photo, tarball, cargo}},
		{"fndr", []indexer.Entry{mainGo, readme}},
		{"readme", []indexer.Entry{readme}},
		{"FNDR/MAIN", []indexer.Entry{mainGo}},
		{"home", nil}, // in the folders above the one searched: everywhere, so nowhere
		{".go|dirs", []indexer.Entry{mainGo, goDir, fakeGo}},
		{" image | archive ", []indexer.Entry{photo, tarball}},
		{"image|dirs|code", []indexer.Entry{mainGo, photo, goDir, fakeGo}}, // categories apart, one set
	} {
		tag, err := Parse(tc.tag)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.tag, err)
			continue
		}
		var got []indexer.Entry
		for _, e := range all {
			if tag.Match(e, under) {
				got = append(got, e)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("%q matched %v\nwant %v", tc.tag, names(got), names(tc.want))
		}
	}
}

func names(es []indexer.Entry) []string {
	ns := make([]string, len(es))
	for i := range es {
		ns[i] = es[i].Name()
	}
	return ns
}

func TestParse(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{".GO", ".go"},
		{" Dirs ", "dirs"},
		{">1MB", "size>1mb"},
		{"size<10k", "size<10k"},
		{".go | DIRS", ".go|dirs"},
		{"size", "size"}, // a word, not a size
		{".", "."},       // a piece of path
		{"./x", "./x"},   // not an extension
		{"my notes", "my notes"},
	} {
		tag, err := Parse(tc.in)
		if err != nil || tag.String() != tc.want {
			t.Errorf("Parse(%q) = %q, %v; want %q", tc.in, tag.String(), err, tc.want)
		}
	}
	for _, bad := range []string{"", " ", "|", ".go|", "|dirs", ">", "size>", ">1xb", ">mb", ">1..2mb",
		">nan", ">inf", ">1e9", ">-1", ">+1", ">0x10", ">99999999tb"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted it", bad)
		}
	}
	if _, err := Parse(".go|"); !errors.Is(err, ErrEmpty) {
		t.Errorf("Parse(%q): %v, want ErrEmpty", ".go|", err)
	}
}

func TestToggle(t *testing.T) {
	gog, _ := Parse(".go")
	dirs, _ := Parse("dirs")
	gogAgain, _ := Parse(" .GO ")

	s := Stack{}.Toggle(gog).Toggle(dirs)
	if len(s) != 2 {
		t.Fatalf("two tags: %v", s)
	}
	off := s.Toggle(gogAgain)
	if len(off) != 1 || off[0].String() != "dirs" {
		t.Errorf("the same tag again takes it off: %v", off)
	}
	if len(s) != 2 || s[0].String() != ".go" {
		t.Errorf("Toggle changed the stack it was called on: %v", s)
	}

	// AND: a .go folder is the only one both .go and dirs.
	if !s.Match(dir("dev/tools.go"), under) || s.Match(file("main.go", 1), under) || s.Match(dir("dev"), under) {
		t.Error("a stack must match every tag")
	}
	if !(Stack{}).Match(file("x", 1), under) {
		t.Error("no tags must match everything")
	}
}

// TestToggleNeverShares: a stack handed to a search must not change when a
// tag is added to it later, even if its array had room to spare.
func TestToggleNeverShares(t *testing.T) {
	a, _ := Parse("a")
	b, _ := Parse("b")
	c, _ := Parse("c")
	base := make(Stack, 1, 4)
	base[0] = a
	searched := base.Toggle(b)
	_ = base.Toggle(c)
	if searched[1].String() != "b" {
		t.Errorf("a later Toggle wrote into a stack in use: %v", searched)
	}
}

// FuzzParse: Parse never panics, and a tag shown and typed again is the
// same tag: what the chip says is what it does.
func FuzzParse(f *testing.F) {
	for _, s := range []string{".go", "dirs|.go", "size>1.5MB", "<10k", "image", "foo bar", "|", ">1..mb", "Ǆ.GO"} {
		f.Add(s)
	}
	entries := []indexer.Entry{file("a/main.go", 10), file("b.TAR.GZ", 2e6), dir("dev/x.go"), file("ǆ.go", 1)}
	f.Fuzz(func(t *testing.T, s string) {
		tag, err := Parse(s)
		if err != nil {
			return
		}
		again, err := Parse(tag.String())
		if err != nil || again.String() != tag.String() {
			t.Fatalf("%q shows as %q, which parses to %q, %v", s, tag.String(), again.String(), err)
		}
		for _, e := range entries {
			if tag.Match(e, under) != again.Match(e, under) {
				t.Fatalf("%q and %q disagree on %s", s, tag.String(), e.Path)
			}
		}
	})
}

// BenchmarkSearch: what tags cost a whole search over 100k entries, as
// the TUI makes it: with a query (tags only see what it found), and with
// tags alone (every entry is tested, and ranked by time).
func BenchmarkSearch(b *testing.B) {
	es := benchIndex()
	code, err := Parse("code|doc")
	if err != nil {
		b.Fatal(err)
	}
	s := Stack{code}
	keep := func(e indexer.Entry) bool { return s.Match(e, under) }
	for _, q := range []indexer.Query{
		{Text: "pkgmain", Under: under},
		{Text: "pkgmain", Under: under, Keep: keep},
		{Under: under, Keep: keep},
	} {
		name := fmt.Sprintf("text=%q/tags=%t", q.Text, q.Keep != nil)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := indexer.Search(context.Background(), es, q); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchIndex() []indexer.Entry {
	kinds := []string{"main.go", "README.md", "photo.jpg", "notes.txt", "lib.rs", "archive.zip", "Makefile"}
	es := make([]indexer.Entry, 100_000)
	for i := range es {
		es[i] = indexer.NewEntry(fmt.Sprintf("%s/dev/project%d/pkg/%s", under, i%300, kinds[i%len(kinds)]), int64(i*37), int64(i), false)
	}
	return es
}

// BenchmarkMatch: a stack of three tags over 100k entries, the work it
// adds to every key typed. It must not allocate.
func BenchmarkMatch(b *testing.B) {
	es := benchIndex()
	var s Stack
	for _, t := range []string{"code|doc", "size>1k", "project1"} {
		tag, err := Parse(t)
		if err != nil {
			b.Fatal(err)
		}
		s = s.Toggle(tag)
	}
	b.ReportAllocs()
	n := 0
	for b.Loop() {
		n = 0
		for i := range es {
			if s.Match(es[i], under) {
				n++
			}
		}
	}
	b.ReportMetric(float64(n), "matched")
}
