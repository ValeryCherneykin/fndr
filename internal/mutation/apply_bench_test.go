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

func BenchmarkCopy(b *testing.B) {
	dir := b.TempDir()
	const size = 8 << 20
	if err := os.WriteFile(filepath.Join(dir, "source"), make([]byte, size), 0o600); err != nil {
		b.Fatal(err)
	}
	dst := filepath.Join(dir, "copy")
	plan := Plan{{Kind: OpCopy, From: "source", To: "copy"}}
	b.SetBytes(size)
	for b.Loop() {
		if _, err := Run(b.Context(), dir, plan, nil, nil); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := os.Remove(dst); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}
