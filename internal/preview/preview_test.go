package preview

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBuildText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.go")
	write(t, path, "package main\r\n\nfunc main() {}\nline 4\nline 5\n")

	p, err := Build(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"package main", "", "func main() {}"}
	if p.Kind != Text || !slices.Equal(p.Lines, want) {
		t.Errorf("Build = kind %d lines %q, want text %q", p.Kind, p.Lines, want)
	}
}

// TestBuildTextIsSafe: a file must never be able to drive the terminal.
func TestBuildTextIsSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evil.txt")
	write(t, path, "hi\x1b[2Jthere\x07\tok\u202e!\n")

	p, err := Build(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Lines[0]; strings.ContainsAny(got, "\x1b\x07") || got != "hi·[2Jthere·\tok·!" {
		t.Errorf("line = %q, want control characters replaced and the tab kept", got)
	}
}

func TestBuildDir(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "b.go"), "")
	write(t, filepath.Join(dir, "sub", "x"), "")

	p, err := Build(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(p.Entries))
	for _, e := range p.Entries {
		names = append(names, e.Name())
	}
	if p.Kind != Dir || !slices.Equal(names, []string{"sub", "b.go"}) {
		t.Errorf("Build = kind %d entries %q, want the folder in display order", p.Kind, names)
	}
}

func TestBuildCard(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct{ content, want string }{
		"doc.pdf":  {"%PDF-1.7\n%\xe2\xe3\xcf\xd3\n", "PDF document"},
		"a.zip":    {"PK\x03\x04\x14\x00", "ZIP archive"}, // images have their own Kind: TestBuildImage
		"fndr":     {"\xcf\xfa\xed\xfe\x0c\x00", "program"},
		"blob.bin": {"\x00\x01\x02\x03", "binary file"},
	}
	for name, c := range cases {
		path := filepath.Join(dir, name)
		write(t, path, c.content)
		p, err := Build(path, 10)
		if err != nil {
			t.Fatal(err)
		}
		if p.Kind != Card || p.Type != c.want || p.Size != int64(len(c.content)) || p.ModTime.IsZero() {
			t.Errorf("%s: %+v, want a card for %q with size and date", name, p, c.want)
		}
	}
}

func TestBuildMissing(t *testing.T) {
	if _, err := Build(filepath.Join(t.TempDir(), "gone"), 10); err == nil {
		t.Error("Build of a missing file succeeded")
	}
}

func TestTypeName(t *testing.T) {
	tests := map[string]string{
		"%PDF-1.4":                 "PDF document",
		"\xff\xd8\xff\xe0\x00\x10": "JPEG image",
		"PK\x03\x04\x14\x00":       "ZIP archive",
		"SQLite format 3\x00\x10":  "SQLite database",
		"PK\x03":                   "binary file", // cut short: not a whole signature
		"\x00\x00\x00\x00":         "binary file",
		"":                         "binary file",
	}
	for head, want := range tests {
		if got := typeName([]byte(head)); got != want {
			t.Errorf("typeName(%q) = %q, want %q", head, got, want)
		}
	}
}
