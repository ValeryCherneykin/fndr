package mutation

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// BenchmarkApply: 1000 renames and back, the cost of a large :w; it is
// what the steps Run records (an lstat each) add to.
func BenchmarkApply(b *testing.B) {
	dir := b.TempDir()
	there, back := make(Plan, 1000), make(Plan, 1000)
	for i := range there {
		a, z := "a"+strconv.Itoa(i), "z"+strconv.Itoa(i)
		if err := os.WriteFile(filepath.Join(dir, a), nil, 0o600); err != nil {
			b.Fatal(err)
		}
		there[i] = Op{Kind: OpRename, From: a, To: z}
		back[i] = Op{Kind: OpRename, From: z, To: a}
	}
	for b.Loop() {
		if _, err := Apply(dir, there, nil); err != nil {
			b.Fatal(err)
		}
		if _, err := Apply(dir, back, nil); err != nil {
			b.Fatal(err)
		}
	}
}
