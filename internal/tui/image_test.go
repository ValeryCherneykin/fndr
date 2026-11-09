package tui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestDetectGraphics(t *testing.T) {
	tests := []struct {
		env            map[string]string
		graphics, tmux bool
	}{
		{map[string]string{"TERM": "xterm-ghostty"}, true, false},
		{map[string]string{"TERM": "xterm-kitty"}, true, false},
		{map[string]string{"TERM_PROGRAM": "ghostty"}, true, false},
		// Inside tmux TERM is tmux's, but Ghostty's own variable is inherited.
		{map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/x", "GHOSTTY_RESOURCES_DIR": "/a"}, true, true},
		{map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "Apple_Terminal"}, false, false},
		{map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/x"}, false, true},
	}
	for _, tt := range tests {
		g, x := detectGraphics(func(k string) string { return tt.env[k] })
		if g != tt.graphics || x != tt.tmux {
			t.Errorf("%v: graphics %v tmux %v, want %v %v", tt.env, g, x, tt.graphics, tt.tmux)
		}
	}
}

func TestFit(t *testing.T) {
	tests := []struct {
		w, h, maxCols, maxRows int
		cols, rows             int
	}{
		{100, 100, 40, 40, 40, 20}, // square: twice as many columns as rows
		{200, 100, 40, 40, 40, 10}, // wide: limited by width
		{100, 400, 40, 20, 10, 20}, // tall: limited by height
		{1, 1, 40, 40, 40, 20},
		{100, 1, 40, 40, 40, 1}, // a sliver still gets a row
		{0, 10, 40, 40, 0, 0},   // nothing to show
	}
	for _, tt := range tests {
		c, r := fit(tt.w, tt.h, tt.maxCols, tt.maxRows)
		if c != tt.cols || r != tt.rows {
			t.Errorf("fit(%d×%d in %d×%d) = %d×%d, want %d×%d", tt.w, tt.h, tt.maxCols, tt.maxRows, c, r, tt.cols, tt.rows)
		}
	}
}

func pngFile(t *testing.T, dir string, w, h int) string {
	t.Helper()
	path := filepath.Join(dir, "pic.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildImage(t *testing.T) {
	path := pngFile(t, t.TempDir(), 200, 100)

	img, err := buildImage(path, 7, 40, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	if img.cols != 40 || img.rows != 10 {
		t.Errorf("placed at %d×%d, want 40×10", img.cols, img.rows)
	}
	for _, want := range []string{"\x1b_G", "a=T", "i=7", "U=1", "c=40", "r=10", "q=2"} {
		if !strings.Contains(img.seq, want) {
			t.Errorf("sequence lacks %q", want)
		}
	}

	// Through tmux every chunk is wrapped, ESC doubled, or tmux drops it.
	wrapped, err := buildImage(path, 7, 40, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(wrapped.seq, "\x1bPtmux;\x1b\x1b_G") {
		t.Errorf("tmux sequence starts %q, want a passthrough", wrapped.seq[:20])
	}
}

// TestImageLinesFillTheirCells: the placeholders must take exactly cols
// cells per row, or the pane border would move.
func TestImageLinesFillTheirCells(t *testing.T) {
	pi := &paneImage{id: 42, cols: 12, rows: 4}
	lines := pi.lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 12 {
			t.Errorf("line %d is %d cells, want 12", i, w)
		}
	}
	if !strings.Contains(lines[0], "38;5;42m") {
		t.Errorf("line %q: want the image id as a 256-colour foreground", lines[0])
	}
}

// TestPictureInPane: in a terminal that can show pictures, a PNG is sent
// once and drawn with placeholders; the layout stays exact.
func TestPictureInPane(t *testing.T) {
	m, root := browseModel(t, "")
	path := pngFile(t, root, 64, 64)
	m.graphics = true
	send(m, m.load(m.cwd)())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.selectName(t, "pic.png")

	// Run the preview by hand to catch the picture's sequence, which send
	// would hand to the runtime.
	key(m, 'v', tea.ModCtrl)
	e := m.paneCache[path]
	if e.img == nil {
		t.Fatal("no picture prepared for the pane")
	}
	view := m.View().Content
	if !strings.ContainsRune(view, '\U0010EEEE') {
		t.Error("the pane draws no placeholders")
	}
	for i, l := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(l); w != 120 {
			t.Errorf("line %d is %d cells, want 120", i, w)
		}
	}
}

// TestNoGraphicsShowsCard: elsewhere a picture is a card, as before.
func TestNoGraphicsShowsCard(t *testing.T) {
	m, root := browseModel(t, "")
	pngFile(t, root, 64, 64)
	send(m, m.load(m.cwd)())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.selectName(t, "pic.png")
	key(m, 'v', tea.ModCtrl)

	if got := paneShows(t, m); got[1] != "PNG image" {
		t.Errorf("pane %q, want the card", got[:3])
	}
}

// TestImageIDReuse: when an id comes round again, the picture that held it
// is forgotten, so it is sent again rather than showing the new one.
func TestImageIDReuse(t *testing.T) {
	m := testModel(t)
	m.paneCache = map[string]paneEntry{"/old.png": {img: &paneImage{id: firstImageID}}}
	m.imageOwner[firstImageID] = "/old.png"
	m.imageSeq = 255 // the next id wraps round

	if id := m.nextImageID("/new.png"); id != firstImageID {
		t.Fatalf("id %d, want the cycle back at %d", id, firstImageID)
	}
	if m.paneCache["/old.png"].img != nil {
		t.Error("the old picture still claims the id")
	}
	if id := testModel(t).nextImageID("/a.png"); id != firstImageID {
		t.Errorf("first id %d, want %d: never a basic colour", id, firstImageID)
	}
}

// picModel is a model showing pic.png in the pane, in a terminal that can
// show pictures.
func picModel(t *testing.T) (m *Model, path string) {
	t.Helper()
	m, root := browseModel(t, "")
	path = pngFile(t, root, 64, 64)
	m.graphics = true
	send(m, m.load(m.cwd)())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.selectName(t, "pic.png")
	key(m, 'v', tea.ModCtrl)
	if m.paneCache[path].img == nil {
		t.Fatal("no picture at first")
	}
	return m, path
}

// TestPictureComesBack: after more pictures than there are ids, the first
// one's id belongs to another. Coming back to it, the picture is sent
// again, not left as a card for the rest of the session.
func TestPictureComesBack(t *testing.T) {
	m, path := picModel(t)
	m.imageSeq = m.paneCache[path].img.id - 1
	m.nextImageID("/elsewhere.png") // the cycle comes round
	if m.paneCache[path].img != nil {
		t.Fatal("the picture still claims an id it lost")
	}
	away, back := tea.KeyDown, tea.KeyUp // away to another row and back, by keys
	if m.cursor > 0 {
		away, back = back, away
	}
	key(m, away, 0)
	key(m, back, 0)
	if m.selectedPath() != path {
		t.Fatalf("selection on %s, want %s", m.selectedPath(), path)
	}
	if m.paneCache[path].img == nil {
		t.Error("coming back shows the card for good: the picture was never sent again")
	}
}

// TestPictureFollowsPaneSize: zooming a tmux pane makes the preview pane
// bigger; the picture is sent again to fill it, not left at its old size.
func TestPictureFollowsPaneSize(t *testing.T) {
	m, path := picModel(t)
	small := *m.paneCache[path].img
	send(m, tea.WindowSizeMsg{Width: 200, Height: 50})
	big := m.paneCache[path].img
	if big == nil || big.rows <= small.rows {
		t.Errorf("after the pane grew the picture is %+v, was %dx%d", big, small.cols, small.rows)
	}
	if b := m.imageBox(); big != nil && (big.cols > b.cols || big.rows > b.rows) {
		t.Errorf("picture %dx%d overflows its room %+v", big.cols, big.rows, b)
	}
}
