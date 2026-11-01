package indexer

import (
	"path/filepath"
	"strconv"
	"testing"
	"unsafe"
)

// TestEntrySize fails when Entry grows. Every byte added here is multiplied by
// the number of indexed files, so growth must be a deliberate decision: if it
// is, update the limit together with the comment on Entry.
func TestEntrySize(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("size budget is defined for 64-bit platforms")
	}

	const limit = 40
	if got := unsafe.Sizeof(Entry{}); got > limit {
		t.Errorf("unsafe.Sizeof(Entry{}) = %d bytes, want <= %d; check field order before raising the limit", got, limit)
	}
}

func TestEntryName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/Users/valera/dev/main.go", want: "main.go"},
		{path: "/Users/valera/dev", want: "dev"},
		{path: "/main.go", want: "main.go"},
		{path: "/", want: "/"},
		{path: "/Users/valera/.zshrc", want: ".zshrc"},
		{path: "/Users/valera/archive.tar.gz", want: "archive.tar.gz"},
		{path: "/Users/valera/мои файлы/отчёт.pdf", want: "отчёт.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			e := NewEntry(tt.path, 0, 0, false)
			if got := e.Name(); got != tt.want {
				t.Errorf("NewEntry(%q).Name() = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestEntryNameZeroValue(t *testing.T) {
	// An Entry built without NewEntry must degrade to the whole path, never
	// panic or return a truncated name.
	e := Entry{Path: "/tmp/x"}
	if got := e.Name(); got != "/tmp/x" {
		t.Errorf("Entry{}.Name() = %q, want the whole path", got)
	}
}

func TestNewEntryKeepsFields(t *testing.T) {
	e := NewEntry("/a/b", 42, 1700000000000000000, true)
	if e.Path != "/a/b" || e.Size != 42 || e.ModTime != 1700000000000000000 || !e.IsDir {
		t.Errorf("NewEntry stored %+v, fields do not match the arguments", e)
	}
}

// FuzzEntryName checks Name against filepath.Base, the standard library's
// definition of a base name, on arbitrary inputs. Run it with:
//
//	go test -fuzz=FuzzEntryName ./internal/indexer
func FuzzEntryName(f *testing.F) {
	for _, seed := range []string{"", "/", "a", "a/b", "//x//", "/a/./b/../c", ".hidden"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		// NewEntry requires an absolute, cleaned path, so build one.
		path := filepath.Clean(string(filepath.Separator) + s)

		got := NewEntry(path, 0, 0, false).Name()
		if want := filepath.Base(path); got != want {
			t.Errorf("Name(%q) = %q, filepath.Base = %q", path, got, want)
		}
	})
}
