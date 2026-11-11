package indexer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// savedEntries is a tree with what trips formats up: shared prefixes of
// every length, names that sort around the separator ("a b" < "a/b" <
// "a0"), non-ASCII, a newline, negative and huge numbers.
func savedEntries(root string) []Entry {
	j := func(p string) string { return filepath.Join(root, p) }
	return []Entry{
		NewEntry(j("a"), 96, 1, true),
		NewEntry(j("a/b"), 0, -5, false),
		NewEntry(j("a b"), 7, 1<<62, false),
		NewEntry(j("a0"), 1<<40, 0, false),
		NewEntry(j("привет/мир.txt"), 12, 3, false),
		NewEntry(j("привет"), 64, 3, true),
		NewEntry(j("line\nbreak"), 1, 2, false),
		NewEntry(j("a/b c/d/e/f/g.go"), 5, 6, false),
	}
}

// sortedCopy is what Load returns for entries: the same, sorted by path.
func sortedCopy(es []Entry) []Entry {
	es = slices.Clone(es)
	slices.SortFunc(es, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	return es
}

func TestSaveLoad(t *testing.T) {
	for _, root := range []string{"/home/u", "/"} {
		path := filepath.Join(t.TempDir(), "cache", "index")
		in := Saved{Root: root, Rules: "v1 rules", Entries: savedEntries(root)}
		if err := Save(path, in); err != nil {
			t.Fatal(err)
		}
		out, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if out.Root != in.Root || out.Rules != in.Rules {
			t.Errorf("root %q, rules %q; want %q, %q", out.Root, out.Rules, in.Root, in.Rules)
		}
		if want := sortedCopy(in.Entries); !slices.Equal(out.Entries, want) {
			t.Errorf("root %s:\n got %v\nwant %v", root, out.Entries, want)
		}
		for _, e := range out.Entries {
			if e.Name() != filepath.Base(e.Path) {
				t.Errorf("Name of %q is %q", e.Path, e.Name())
			}
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("index file is %v: it lists every file name, only the user may read it", perm)
		}
	}
}

func TestSaveLoadEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index")
	if err := Save(path, Saved{Root: "/r"}); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil || s.Root != "/r" || len(s.Entries) != 0 {
		t.Errorf("Load = %+v, %v", s, err)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "none")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err %v, want fs.ErrNotExist", err)
	}
}

// TestLoadRefusesDamage: a file cut short anywhere, or with any one byte
// changed, is refused: never a panic, never entries made up.
func TestLoadRefusesDamage(t *testing.T) {
	good := encode(Saved{Root: "/r", Rules: "x", Entries: savedEntries("/r")})
	for n := range len(good) {
		if _, err := decode(good[:n]); !errors.Is(err, ErrBadSaved) {
			t.Fatalf("cut to %d bytes: err %v, want ErrBadSaved", n, err)
		}
	}
	for i := range good {
		bad := slices.Clone(good)
		bad[i] ^= 0x10
		if _, err := decode(bad); !errors.Is(err, ErrBadSaved) {
			t.Fatalf("byte %d changed: err %v, want ErrBadSaved", i, err)
		}
	}
}

// resealed is data with a fresh checksum: damage the checksum cannot see,
// as a file written wrong rather than broken on the way would be.
func resealed(data []byte) []byte {
	body := data[:len(data)-4]
	return binary.LittleEndian.AppendUint32(slices.Clone(body), crc32.ChecksumIEEE(body))
}

// TestLoadRefusesWhatSearchCannotTrust: even with a checksum that holds,
// paths must be below the root, cleaned, sorted and unique, as everything
// that reads the index assumes.
func TestLoadRefusesWhatSearchCannotTrust(t *testing.T) {
	tests := map[string]Saved{
		"outside the root": {Root: "/r", Entries: []Entry{NewEntry("/other/x", 0, 0, false)}},
		"the root itself":  {Root: "/r", Entries: []Entry{NewEntry("/r", 0, 0, true)}},
		"a sibling prefix": {Root: "/r", Entries: []Entry{NewEntry("/rx/a", 0, 0, false)}},
		"not cleaned":      {Root: "/r", Entries: []Entry{NewEntry("/r/a/../b", 0, 0, false)}},
		"a trailing slash": {Root: "/r", Entries: []Entry{NewEntry("/r/a/", 0, 0, true)}},
		"twice":            {Root: "/r", Entries: []Entry{NewEntry("/r/a", 0, 0, false), NewEntry("/r/a", 0, 0, false)}},
		"a relative root":  {Root: "r", Entries: nil},
		"an unclean root":  {Root: "/r/", Entries: nil},
	}
	for name, s := range tests {
		if _, err := decode(encode(s)); !errors.Is(err, ErrBadSaved) {
			t.Errorf("%s: err %v, want ErrBadSaved", name, err)
		}
	}

	// Another version: refused, not misread.
	data := encode(Saved{Root: "/r"})
	data[len(savedMagic)] = formatVersion + 1
	if _, err := decode(resealed(data)); !errors.Is(err, ErrBadSaved) {
		t.Errorf("another version: err %v, want ErrBadSaved", err)
	}
}

// TestSavedIsSmall: front coding stores what differs from the path before;
// paths in a tree share most of themselves, and the file shows it.
func TestSavedIsSmall(t *testing.T) {
	s := Saved{Root: "/home/user"}
	raw := 0
	for i := range 1000 {
		p := fmt.Sprintf("/home/user/dev/project/internal/package%d/file%d.go", i/10, i)
		s.Entries = append(s.Entries, NewEntry(p, 1234, 1_700_000_000_000_000_000, false))
		raw += len(p)
	}
	if got := len(encode(s)); got > raw/2 {
		t.Errorf("%d bytes for %d bytes of paths: front coding is not working", got, raw)
	}
}

func TestPruneRules(t *testing.T) {
	a, b := DefaultPrune("/home/a"), DefaultPrune("/home/b")
	if a.Rules() == b.Rules() {
		t.Error("other homes, other folders left out, same rules")
	}
	if a.Rules() != DefaultPrune("/home/a").Rules() {
		t.Error("the same rules described two ways")
	}
	if (*Prune)(nil).Rules() == a.Rules() {
		t.Error("no pruning described as pruning")
	}
}

// FuzzDecode: whatever the bytes, decode returns an error or an index it
// would write back byte for byte: there is one way to write each index, so
// anything else accepted would be something encode never made.
func FuzzDecode(f *testing.F) {
	f.Add(encode(Saved{Root: "/r", Rules: "x", Entries: savedEntries("/r")}))
	f.Add(encode(Saved{Root: "/", Entries: savedEntries("/")}))
	f.Add(encode(Saved{Root: "/r"}))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := decode(data)
		if err != nil {
			return
		}
		if again := encode(s); !bytes.Equal(again, data) {
			t.Fatalf("accepted bytes encode would not write:\n got %q\nwant %q", data, again)
		}
		// decode checks paths by hand, and only what differs from the
		// path before; filepath is the reference it must agree with.
		for _, e := range s.Entries {
			rel, err := filepath.Rel(s.Root, e.Path)
			if filepath.Clean(e.Path) != e.Path || err != nil || !filepath.IsLocal(rel) {
				t.Fatalf("accepted %q below %q", e.Path, s.Root)
			}
		}
	})
}

// BenchmarkLoad measures what a start costs before search works: reading
// and checking a saved index of 100k entries shaped like a home.
func BenchmarkLoad(b *testing.B) {
	s := Saved{Root: "/Users/someone"}
	for i := range 100_000 {
		p := fmt.Sprintf("/Users/someone/dev/project%d/internal/pkg%d/file_%d.go", i/1000, i/20, i)
		s.Entries = append(s.Entries, NewEntry(p, int64(i), 1_700_000_000_000_000_000+int64(i), i%20 == 0))
	}
	path := filepath.Join(b.TempDir(), "index")
	if err := Save(path, s); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Load(path); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSavedHome saves and loads the index of a real tree, as the start
// would: FNDR_BENCH_DIR=$HOME go test -bench SavedHome -run '^$'.
func BenchmarkSavedHome(b *testing.B) {
	root := os.Getenv("FNDR_BENCH_DIR")
	if root == "" {
		b.Skip("set FNDR_BENCH_DIR to a folder to index")
	}
	home, _ := os.UserHomeDir() //nolint:errcheck // without one, only the rules by name apply
	prune := DefaultPrune(home)
	out := make(chan []Entry)
	go func() {
		defer close(out)
		_, _ = Crawl(context.Background(), root, prune, out) //nolint:errcheck // a partial tree still benchmarks
	}()
	s := Saved{Root: filepath.Clean(root), Rules: prune.Rules()}
	for batch := range out {
		s.Entries = append(s.Entries, batch...)
	}
	path := filepath.Join(b.TempDir(), "index")

	b.Run("save", func(b *testing.B) {
		for b.Loop() {
			if err := Save(path, s); err != nil {
				b.Fatal(err)
			}
		}
	})
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("load", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Load(path); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(s.Entries)), "entries")
		b.ReportMetric(float64(info.Size())/1024, "KiB")
	})
}

func TestUnder(t *testing.T) {
	sorted := sortedCopy(savedEntries("/r"))
	paths := func(es []Entry) []string {
		ps := make([]string, 0, len(es))
		for _, e := range es {
			ps = append(ps, e.Path)
		}
		return ps
	}
	for dir, want := range map[string][]string{
		"/r/a":      {"/r/a/b", "/r/a/b c/d/e/f/g.go"}, // not "/r/a b", not "/r/a0"
		"/r/привет": {"/r/привет/мир.txt"},
		"/r/a/b":    nil, // a file: nothing below it
		"/r/none":   nil,
		"/":         paths(sorted),
		"/r":        paths(sorted),
	} {
		if got := paths(Under(sorted, dir)); !slices.Equal(got, want) {
			t.Errorf("Under %s = %q, want %q", dir, got, want)
		}
	}
}
