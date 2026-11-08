package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestIconFor(t *testing.T) {
	glyph := func(name string, isDir bool) string { return ansi.Strip(iconFor(name, isDir)) }
	byExt := func(ext string) string { return ansi.Strip(iconsByExt[ext]) }
	byName := func(name string) string { return ansi.Strip(iconsByName[name]) }

	tests := []struct {
		name  string
		isDir bool
		want  string
	}{
		{"main.go", false, byExt("go")},
		{"PHOTO.JPG", false, byExt("jpg")},          // extensions match in any case
		{"archive.tar.gz", false, byExt("gz")},      // the last extension decides
		{"Dockerfile", false, byName("Dockerfile")}, // no extension at all
		{"go.mod", false, byName("go.mod")},         // the whole name beats ".mod"
		{"README.md", false, byName("README.md")},   // the whole name beats ".md"
		{"notes.md", false, byExt("md")},
		{"src", true, iconDir},
		{"backup.go", true, iconDir}, // a folder is a folder, whatever its name
		{"data.unknownext", false, iconFile},
		{"noext", false, iconFile},
	}
	for _, tt := range tests {
		if got := glyph(tt.name, tt.isDir); got != tt.want {
			t.Errorf("iconFor(%q, dir=%v) = %q, want %q", tt.name, tt.isDir, got, tt.want)
		}
	}
}

// TestIconTablesAreClean: every key is matched exactly as written, so an
// upper-case extension or a leading dot would be dead weight.
func TestIconTablesAreClean(t *testing.T) {
	for ext := range iconsByExt {
		if ext == "" || ext[0] == '.' || ext != strings.ToLower(ext) {
			t.Errorf("extension key %q: want lower case, no dot", ext)
		}
	}
}
