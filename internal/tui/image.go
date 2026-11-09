package tui

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/ValeryCherneykin/fndr/internal/preview"
)

// Pictures are shown with the kitty graphics protocol, using Unicode
// placeholders:
//
//  1. The picture is sent to the terminal once, under an image id, with a
//     "virtual placement" of so many columns and rows.
//  2. Wherever the screen then holds placeholder characters (U+10EEEE, with
//     diacritics for row and column, coloured with the image id), the
//     terminal paints the matching piece of the picture.
//
// The placeholders are ordinary text to Bubble Tea, so the picture lives in
// the same cell grid as everything else: it is redrawn, scrolled and erased
// with the pane, needs no cursor tricks, and works inside tmux, which only
// has to pass the transmission through once.

// imageMaxSide is how many pixels the long side of a picture is shrunk to
// before it is sent: more than a preview pane shows on any common screen.
const imageMaxSide = 800

// paneImage is a picture sent to the terminal, ready to be drawn.
type paneImage struct {
	id         int // firstImageID-255: the id travels as a 256-colour index, see lines
	cols, rows int
	seq        string // transmission and placement, written to the terminal once
}

// box is the room a picture is fit into, in cells: the pane, less its
// border and margins.
type box struct{ cols, rows int }

// paneImageMsg carries a picture prepared for the terminal, or nil when it
// could not be decoded (the card is shown instead). box is the room it was
// prepared for: if the pane has changed size since, it is dropped.
type paneImageMsg struct {
	path string
	box  box
	img  *paneImage
}

// detectGraphics reports whether the terminal can show pictures with
// Unicode placeholders, and whether output goes through tmux.
//
// The environment is the only cheap, synchronous signal. Querying the
// terminal would be exact, but its answer arrives as input, mixed with the
// user's keys. Inside tmux, TERM is tmux's own, but the terminal's variables
// are inherited by the tmux server from the terminal that started it.
func detectGraphics(getenv func(string) string) (graphics, tmux bool) {
	tmux = getenv("TMUX") != ""
	switch {
	case getenv("KITTY_WINDOW_ID") != "", getenv("GHOSTTY_RESOURCES_DIR") != "":
		graphics = true
	case getenv("TERM") == "xterm-kitty", getenv("TERM") == "xterm-ghostty":
		graphics = true
	case strings.EqualFold(getenv("TERM_PROGRAM"), "ghostty"):
		graphics = true
	}
	return graphics, tmux
}

// allowPassthrough lets this tmux pane forward the picture escape
// sequences to the terminal; tmux drops them otherwise. Only this pane is
// changed, the way yazi does it, so the user configures nothing.
func allowPassthrough() tea.Msg {
	_ = exec.Command("tmux", "set", "-p", "allow-passthrough", "on").Run() //nolint:errcheck,gosec // best effort: without it pictures just do not appear
	return nil
}

// Image ids are 256-colour indexes from 16 up. Indexes 0-15 are the basic
// colours, which renderers are free to write in their short form (33 instead
// of 38;5;3), and the terminal reads the id only from the long one.
const firstImageID = 16

// nextImageID hands out image ids in a cycle and makes path their owner.
// An id handed out again belongs to a new picture: the old owner's picture,
// shown or on its way, is forgotten, and wantImage asks for it again when
// it is next shown.
func (m *Model) nextImageID(path string) int {
	if m.imageSeq < firstImageID || m.imageSeq >= 255 {
		m.imageSeq = firstImageID - 1
	}
	m.imageSeq++
	id := m.imageSeq
	if old := m.imageOwner[id]; old != "" {
		if e, ok := m.paneCache[old]; ok && (e.img == nil || e.img.id == id) {
			e.img, e.imgFor = nil, box{}
			m.paneCache[old] = e
		}
	}
	m.imageOwner[id] = path
	return id
}

// loadImage prepares the picture at path for the pane, off the UI
// goroutine. It is fit into b keeping its shape.
func loadImage(path string, id int, b box, tmux bool) tea.Cmd {
	return func() tea.Msg {
		img, err := buildImage(path, id, b.cols, b.rows, tmux)
		if err != nil {
			return paneImageMsg{path: path, box: b}
		}
		return paneImageMsg{path: path, box: b, img: img}
	}
}

func buildImage(path string, id, maxCols, maxRows int, tmux bool) (*paneImage, error) {
	img, err := preview.LoadImage(path, imageMaxSide)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	cols, rows := fit(b.Dx(), b.Dy(), maxCols, maxRows)

	opts := &kitty.Options{
		Action:           kitty.TransmitAndPut,
		ID:               id,
		Format:           kitty.PNG,
		Transmission:     kitty.Direct,
		Chunk:            true,
		Quiet:            2, // never answer: replies would arrive as keystrokes
		VirtualPlacement: true,
		Columns:          cols,
		Rows:             rows,
	}
	if tmux {
		opts.ChunkFormatter = ansi.TmuxPassthrough
	}
	var seq strings.Builder
	if err := kitty.EncodeGraphics(&seq, img, opts); err != nil {
		return nil, fmt.Errorf("image: %w", err)
	}
	return &paneImage{id: id, cols: cols, rows: rows, seq: seq.String()}, nil
}

// fit returns the cells a w×h pixel picture takes in a box of maxCols ×
// maxRows cells, keeping its shape. A terminal cell is about twice as tall
// as it is wide, so a square picture takes twice as many columns as rows.
func fit(w, h, maxCols, maxRows int) (cols, rows int) {
	if w <= 0 || h <= 0 || maxCols <= 0 || maxRows <= 0 {
		return 0, 0
	}
	cols = maxCols
	rows = (h*cols + w) / (2 * w) // h/w × cols ÷ 2, rounded
	if rows > maxRows {
		rows = maxRows
		cols = (2*w*rows + h/2) / h
	}
	return max(min(cols, maxCols), 1), max(rows, 1)
}

// lines draws the picture as placeholder cells. Each cell names its row and
// column with combining diacritics; the foreground colour names the image.
// A 256-colour index is used, not RGB, so the id survives terminals and
// multiplexers that reduce true colour.
func (pi *paneImage) lines() []string {
	st := lipgloss.NewStyle().Foreground(lipgloss.Color(strconv.Itoa(pi.id)))
	out := make([]string, pi.rows)
	var b strings.Builder
	for r := range pi.rows {
		b.Reset()
		for c := range pi.cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
		}
		out[r] = st.Render(b.String())
	}
	return out
}

// deleteImages frees every picture fndr sent, so they do not linger in the
// terminal's memory after it exits.
func deleteImages(tmux bool) string {
	seq := ansi.KittyGraphics(nil, "a=d", "d=A", "q=2")
	if tmux {
		seq = ansi.TmuxPassthrough(seq)
	}
	return seq
}
