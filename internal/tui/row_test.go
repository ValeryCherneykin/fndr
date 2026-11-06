package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

func result(path string, positions ...int) indexer.Result {
	return indexer.Result{Entry: indexer.NewEntry(path, 0, 0, false), Positions: positions}
}

// plain renders a row and strips the colours, leaving what the user reads.
func plain(r indexer.Result, root string, width int, selected bool) string {
	return ansi.Strip(renderRow(&r, root, width, selected))
}

func TestRenderRowLayout(t *testing.T) {
	r := result("/home/v/dev/fndr/cmd/fndr/main.go")
	got := plain(r, "/home/v/dev", 50, true)

	if !strings.HasPrefix(got, "▎ "+iconFile+" main.go ") {
		t.Errorf("row = %q, want marker, icon, then the name", got)
	}
	if !strings.HasSuffix(got, "fndr/cmd/fndr ") {
		t.Errorf("row = %q, want the directory relative to root, right-aligned", got)
	}
}

func TestRenderRowUnselectedHasNoMarker(t *testing.T) {
	got := plain(result("/r/a.go"), "/r", 30, false)
	if strings.Contains(got, "▎") || !strings.HasPrefix(got, "  ") {
		t.Errorf("row = %q, want a blank marker column", got)
	}
}

func TestRenderRowDirectoryGivesWayFirst(t *testing.T) {
	r := result("/r/very/long/directory/path/here/main.go")

	got := plain(r, "/r", 30, false)
	if !strings.Contains(got, "main.go") {
		t.Errorf("row = %q, the name must survive", got)
	}
	if !strings.Contains(got, "…") || !strings.HasSuffix(strings.TrimRight(got, " "), "here") {
		t.Errorf("row = %q, want the directory cut from the left, keeping its tail", got)
	}

	// Narrower still: the directory disappears, the name stays whole.
	got = plain(r, "/r", 16, false)
	if !strings.Contains(got, "main.go") || strings.Contains(got, "here") {
		t.Errorf("row = %q, want only the name", got)
	}
}

func TestRenderRowLongNameIsCut(t *testing.T) {
	got := plain(result("/r/an_extremely_long_file_name.go"), "/r", 20, false)
	if !strings.Contains(got, "…") || strings.Contains(got, ".go") {
		t.Errorf("row = %q, want the name cut with an ellipsis", got)
	}
}

func TestRenderRowEntryInRootHasNoDirectory(t *testing.T) {
	got := plain(result("/r/main.go"), "/r", 30, false)
	if strings.TrimRight(got, " ") != "  "+iconFile+" main.go" {
		t.Errorf("row = %q, want just the name", got)
	}
}

// TestRenderRowExactWidth is the layout guarantee: a row is always exactly
// width cells, whatever the name, the script or the terminal size. A row one
// cell too wide wraps and wrecks the whole screen.
func TestRenderRowExactWidth(t *testing.T) {
	paths := []string{
		"/r/main.go",
		"/r/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/main.go",
		"/r/мои документы/отчёт за год.pdf",
		"/r/日本語/ファイル名.txt",
		"/r/" + strings.Repeat("x", 200),
	}
	for _, p := range paths {
		for _, w := range []int{1, 5, 6, 10, 20, 37, 80, 200} {
			for _, sel := range []bool{false, true} {
				r := result(p, len(p)-1) // highlight the last byte
				row := renderRow(&r, "/r", w, sel)
				if got := ansi.StringWidth(row); got != w {
					t.Errorf("path %q, width %d: row is %d cells: %q", p, w, got, ansi.Strip(row))
				}
			}
		}
	}
}

func TestStyleRangeKeepsText(t *testing.T) {
	// Highlighting changes colours only, never the characters.
	path := "/r/Отчёт-main.go"
	got := ansi.Strip(styleRange(path, 3, len(path), []int{3, 5, 14}, &stName, &stNameHit))
	if got != "Отчёт-main.go" {
		t.Errorf("styleRange text = %q, want %q", got, "Отчёт-main.go")
	}
}
