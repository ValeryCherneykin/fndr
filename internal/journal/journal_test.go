package journal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// some is records with the shapes that break a careless framing: empty,
// a newline, a NUL, the header itself.
var some = [][]byte{[]byte("first"), {}, []byte("two\nlines\x00"), []byte(header)}

func appendAll(t *testing.T, path string, recs [][]byte) {
	t.Helper()
	for _, r := range recs {
		if err := Append(path, r); err != nil {
			t.Fatal(err)
		}
	}
}

func mustRead(t *testing.T, path string) [][]byte {
	t.Helper()
	recs, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

func equal(a, b [][]byte) bool { return slices.EqualFunc(a, b, bytes.Equal) }

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	if recs := mustRead(t, path); recs != nil {
		t.Fatalf("no journal: %q", recs)
	}
	appendAll(t, path, some)
	if got := mustRead(t, path); !equal(got, some) {
		t.Errorf("read %q, want %q", got, some)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v: want 0600, it names the user's files", info.Mode(), err)
	}
}

// TestTornAnywhere: a crash can stop an append at any byte. Cut there,
// the journal reads as the records wholly before the cut, and the next
// append goes after them, never after the torn bytes: the record is read
// back, and so is every one before it.
func TestTornAnywhere(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "full")
	appendAll(t, full, some)
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	ends := make([]int, 1, 1+len(some)) // where each record ends
	ends[0] = len(header)
	for _, r := range some {
		ends = append(ends, ends[len(ends)-1]+frame+len(r))
	}

	path := filepath.Join(dir, "journal")
	for cut := 0; cut <= len(data); cut++ {
		if err := os.WriteFile(path, data[:cut], 0o600); err != nil { //nolint:gosec // the test's own temp file
			t.Fatal(err)
		}
		whole := 0
		for whole < len(some) && ends[whole+1] <= cut {
			whole++
		}
		if got := mustRead(t, path); !equal(got, some[:whole]) {
			t.Fatalf("cut at %d: read %q, want the first %d", cut, got, whole)
		}
		if err := Append(path, []byte("after")); err != nil {
			t.Fatalf("cut at %d: append: %v", cut, err)
		}
		want := append(slices.Clone(some[:whole]), []byte("after"))
		if got := mustRead(t, path); !equal(got, want) {
			t.Fatalf("cut at %d, then appended: read %q, want %q", cut, got, want)
		}
	}
}

// TestDamage: a byte changed in a record ends the journal there: the
// records before it are read, it and what follows are not, as nothing
// past a bad frame can be trusted to be framed. The next append cuts them
// off: written over the bad record's start, it would leave the whole ones
// after it to be read again, as if appended after it.
func TestDamage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	appendAll(t, path, some)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second := len(header) + frame + len(some[0]) // where the second record's frame starts
	for i, at := range map[string]int{"length": second, "checksum": second + 4, "bytes": len(header) + frame} {
		bad := slices.Clone(data)
		bad[at] ^= 0x40
		if err := os.WriteFile(path, bad, 0o600); err != nil { //nolint:gosec // the test's own temp file
			t.Fatal(err)
		}
		want := some[:1]
		if at < second {
			want = nil
		}
		if got := mustRead(t, path); !equal(got, want) {
			t.Errorf("%s damaged: read %q, want %q", i, got, want)
		}
		// As long as the bad record: the whole ones after it would line
		// up again behind the new one, if not cut off.
		same := bytes.Repeat([]byte("x"), len(some[len(want)]))
		if err := Append(path, same); err != nil {
			t.Fatal(err)
		}
		want = append(slices.Clone(want), same)
		if got := mustRead(t, path); !equal(got, want) {
			t.Errorf("%s damaged, then appended: read %q, want %q", i, got, want)
		}
	}
}

// TestNotAJournal: a file that is not a journal is never cut or written,
// whatever is asked: it may be the user's.
func TestNotAJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	mine := []byte("my notes\n")
	if err := os.WriteFile(path, mine, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, []byte("x")); !errors.Is(err, ErrNotJournal) {
		t.Errorf("append: %v, want ErrNotJournal", err)
	}
	if _, err := Read(path); !errors.Is(err, ErrNotJournal) {
		t.Errorf("read: %v, want ErrNotJournal", err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, mine) {
		t.Errorf("the file is now %q", got)
	}
}

// TestHugeLength: a length past MaxRecord, as a damaged frame may say, is
// the torn tail, read without allocating it; a record that long is refused.
func TestHugeLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	appendAll(t, path, some[:1])
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	var head [frame]byte
	binary.BigEndian.PutUint32(head[:4], 0xFFFFFFFF)
	if _, err := f.Write(head[:]); err != nil {
		t.Fatal(err)
	}
	f.Close() //nolint:errcheck,gosec // test
	if got := mustRead(t, path); !equal(got, some[:1]) {
		t.Errorf("read %q", got)
	}
	if err := Append(path, make([]byte, MaxRecord+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}
}

// TestAppendsAtOnce: fndr in several tmux panes appending together lose
// nothing and tear nothing: the lock takes them one at a time.
func TestAppendsAtOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	const writers, each = 8, 40
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for i := range each {
				if err := Append(path, fmt.Appendf(nil, "%d-%d", w, i)); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	got := mustRead(t, path)
	if len(got) != writers*each {
		t.Fatalf("%d records, want %d", len(got), writers*each)
	}
	seen := map[string]bool{}
	for _, r := range got {
		seen[string(r)] = true
	}
	if len(seen) != writers*each {
		t.Errorf("%d different records, want %d", len(seen), writers*each)
	}
}

// FuzzParse: whatever bytes are on the disk, parse never fails on a
// journal's header, and what it reads, framed again, is exactly the bytes
// it says the records end at.
func FuzzParse(f *testing.F) {
	f.Add([]byte(header))
	f.Add([]byte(header[:5]))
	f.Add([]byte("garbage"))
	var b bytes.Buffer
	b.WriteString(header)
	for _, r := range some {
		var head [frame]byte
		binary.BigEndian.PutUint32(head[:4], uint32(len(r))) //nolint:gosec // test
		binary.BigEndian.PutUint32(head[4:], crc32.Checksum(r, castagnoli))
		b.Write(head[:])
		b.Write(r)
	}
	f.Add(b.Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		recs, end, err := parse(data)
		if err != nil {
			if bytes.HasPrefix(data, []byte(header)) {
				t.Fatalf("a journal's header, and %v", err)
			}
			return
		}
		if end > len(data) || (end == 0) != (len(recs) == 0 && !bytes.HasPrefix(data, []byte(header))) {
			t.Fatalf("end %d of %d, %d records", end, len(data), len(recs))
		}
		var again bytes.Buffer
		if end > 0 {
			again.WriteString(header)
		}
		for _, r := range recs {
			var head [frame]byte
			binary.BigEndian.PutUint32(head[:4], uint32(len(r))) //nolint:gosec // test
			binary.BigEndian.PutUint32(head[4:], crc32.Checksum(r, castagnoli))
			again.Write(head[:])
			again.Write(r)
		}
		if !bytes.Equal(again.Bytes(), data[:end]) {
			t.Fatalf("records %q framed again are not data[:%d]", recs, end)
		}
	})
}
