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

func TestClassify(t *testing.T) {
	dir := t.TempDir()
	files := map[string]struct {
		content string
		want    Kind
	}{
		"Makefile":  {"build:\n\tgo build\n", Text},
		"empty":     {"", Text},
		"run.sh":    {"#!/bin/sh\necho hi\n", Text}, // scripts are edited, not run
		"photo.png": {"\x89PNG\r\n\x1a\n\x00\x00", Other},
		"fndr":      {"\xcf\xfa\xed\xfe\x0c\x00\x00\x01", Program},
		"fndr-elf":  {"\x7fELF\x02\x01\x01\x00", Program},
		// Longer than sniffLen, with a NUL after it: only the head counts.
		"long.txt": {strings.Repeat("a", sniffLen) + "\x00", Text},
	}
	for name, f := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(f.content), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := Classify(path)
		if err != nil || got != f.want {
			t.Errorf("Classify(%s) = %v, %v; want %v, nil", name, got, err, f.want)
		}
	}

	if _, err := Classify(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Errorf("Classify(missing) error = %v, want not-exist", err)
	}
}

func TestLooksLikeProgram(t *testing.T) {
	tests := []struct {
		head string
		want bool
	}{
		{"\x7fELF\x02\x01", true},
		{"\xcf\xfa\xed\xfe\x0c", true},
		{"\xca\xfe\xba\xbe\x00", true},
		{"\xca\xfe\xba\xbf\x00", true},               // universal, 64-bit offsets
		{"MZ\x90\x00\x03\x00\x00\x00\x04\x00", true}, // a Windows .exe: wslview would run it
		{"MZ notes: buy milk", false},                // text that happens to start with MZ
		{"%PDF-1.7", false},
		{"ELF", false},    // not the signature: it starts with \x7f
		{"\x7fEL", false}, // too short to be a whole signature
		{"", false},
	}
	for _, tt := range tests {
		if got := looksLikeProgram([]byte(tt.head)); got != tt.want {
			t.Errorf("looksLikeProgram(%q) = %v, want %v", tt.head, got, tt.want)
		}
	}
}
