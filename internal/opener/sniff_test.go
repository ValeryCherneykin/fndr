package opener

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLooksLikeText(t *testing.T) {
	ru := "привет"
	tests := []struct {
		name string
		head string
		want bool
	}{
		{"empty file", "", true},
		{"go source", "package main\n\nfunc main() {}\n", true},
		{"cyrillic", "# Отчёт\n" + ru, true},
		{"cut inside a character", ru[:len(ru)-1], true},
		{"nul byte", "ELF\x00\x01\x02", false},
		{"invalid utf-8", "abc\xff\xfedef", false},
		{"pdf header", "%PDF-1.7\n%\xe2\xe3\xcf\xd3\n", false},
		{"png header", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", false},
		{"invalid byte at the very end", "abc\xff", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeText([]byte(tt.head)); got != tt.want {
				t.Errorf("looksLikeText(%q) = %v, want %v", tt.head, got, tt.want)
			}
		})
	}
}

func TestIsText(t *testing.T) {
	dir := t.TempDir()
	files := map[string]struct {
		content string
		want    bool
	}{
		"Makefile":  {"build:\n\tgo build\n", true},
		"empty":     {"", true},
		"photo.png": {"\x89PNG\r\n\x1a\n\x00\x00", false},
		// Longer than sniffLen, with a NUL after it: only the head counts.
		"long.txt": {strings.Repeat("a", sniffLen) + "\x00", true},
	}
	for name, f := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(f.content), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := IsText(path)
		if err != nil || got != f.want {
			t.Errorf("IsText(%s) = %v, %v; want %v, nil", name, got, err, f.want)
		}
	}

	if _, err := IsText(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Errorf("IsText(missing) error = %v, want not-exist", err)
	}
}
