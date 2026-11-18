package tui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/iterm2"
	"github.com/charmbracelet/x/ansi/sixel"
	"github.com/charmbracelet/x/term"
	"golang.org/x/image/draw"

	"github.com/ValeryCherneykin/fndr/internal/preview"
)

const inlineMaxSide = 400

func buildInlineImage(path string, id int, b box, protocol graphicsProtocol) (*paneImage, error) {
	img, err := preview.LoadImage(path, inlineMaxSide)
	if err != nil {
		return nil, err
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w == 0 || h == 0 || b.cols <= 0 || b.rows <= 0 {
		return nil, errors.New("empty image area")
	}
	p := &paneImage{id: id, protocol: protocol}
	if protocol == graphicsITerm {
		p.cols, p.rows = fit(w, h, b.cols, b.rows)
		var data bytes.Buffer
		encoder := png.Encoder{CompressionLevel: png.BestSpeed}
		if err := encoder.Encode(&data, img); err != nil {
			return nil, err
		}
		content := base64.StdEncoding.AppendEncode(nil, data.Bytes())
		p.seq = ansi.ITerm2(iterm2.File{
			Inline: true, Width: iterm2.Cells(p.cols), Height: iterm2.Cells(p.rows), Content: content,
		})
		return p, nil
	}

	if b.cellWidth <= 0 || b.cellHeight <= 0 {
		return nil, errors.New("unknown terminal cell size")
	}
	pw := min(b.cols*b.cellWidth, inlineMaxSide)
	ph := min(b.rows*b.cellHeight, inlineMaxSide)
	if w*ph > h*pw {
		ph = max(1, h*pw/w)
	} else {
		pw = max(1, w*ph/h)
	}
	p.cols, p.rows = (pw+b.cellWidth-1)/b.cellWidth, (ph+b.cellHeight-1)/b.cellHeight
	scaled := image.NewRGBA(image.Rect(0, 0, pw, ph))
	draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Src, nil)
	var seq strings.Builder
	seq.WriteString("\x1bP0;1q")
	if err := (&sixel.Encoder{}).Encode(&seq, scaled); err != nil {
		return nil, err
	}
	seq.WriteString("\x1b\\")
	p.seq = seq.String()
	return p, nil
}

func (m *Model) probeInline() tea.Cmd {
	if !m.probeGraphics || m.graphicsProbed || !m.paneShown() {
		return nil
	}
	m.graphicsProbed = true
	return tea.Raw(ansi.RequestPrimaryDeviceAttributes)
}

func (m *Model) enableSixel(attrs uv.PrimaryDeviceAttributesEvent) tea.Cmd {
	if !m.probeGraphics || !m.graphicsProbed || m.graphics != graphicsNone ||
		len(attrs) < 2 || !slices.Contains(attrs[1:], 4) {
		return nil
	}
	m.graphics = graphicsSixel
	return requestCellSize()
}

func requestCellSize() tea.Cmd {
	const requestCellPixels, requestWindowPixels = 16, 14
	return tea.Raw(ansi.WindowOp(requestCellPixels) + ansi.WindowOp(requestWindowPixels))
}

func (m *Model) setCellSize(w, h int) bool {
	if w <= 0 || w > 256 || h <= 0 || h > 256 {
		return false
	}
	m.cellWidth, m.cellHeight = w, h
	return true
}

type inlineFrame struct {
	title string
	img   *paneImage
	col   int
}

type inlineClearedMsg struct{}

func (m *Model) syncInline() tea.Cmd {
	if !m.graphics.inline() || m.inlineClearing {
		return nil
	}
	var img *paneImage
	if m.oil == nil && m.help == nil && m.paneShown() && m.paneWant != "" {
		entry := m.paneCache[m.paneWant]
		if entry.imgFor == m.imageBox() {
			img = entry.img
		}
	}
	old := m.inline.Load()
	col := m.listWidth() + 3
	if old != nil {
		if old.img == img && old.col == col {
			return nil
		}
		m.inline.Store(nil)
		m.inlineClearing = true
		return tea.Sequence(tea.ClearScreen, func() tea.Msg { return inlineClearedMsg{} })
	}
	if img != nil {
		m.inlineSeq++
		m.inline.Store(&inlineFrame{title: inlineTitle + strconv.Itoa(m.inlineSeq), col: col, img: img})
	}
	return nil
}

const inlineTitle = "fndr:inline:"

type inlineOutput struct {
	*os.File
	model *Model
}

// TerminalOutput attaches inline graphics after the renderer flushes the corresponding text frame.
func (m *Model) TerminalOutput(out *os.File) io.Writer {
	if m.graphics != graphicsITerm && !m.probeGraphics {
		return out
	}
	if !term.IsTerminal(out.Fd()) {
		m.graphics, m.probeGraphics = graphicsNone, false
		return out
	}
	return &inlineOutput{File: out, model: m}
}

func (o *inlineOutput) Write(p []byte) (int, error) {
	if _, err := o.File.Write(inlineFrameOutput(p, o.model.inline.Load())); err != nil {
		return 0, err
	}
	return len(p), nil
}

func inlineFrameOutput(p []byte, frame *inlineFrame) []byte {
	// Bubble Tea can flush Raw messages before the frame that clears their
	// cells. A private title tag identifies the frame in the renderer's
	// output. Replace the tag before anything reaches the terminal.
	const titleOSC = "\x1b]2;"
	const prefix = titleOSC + inlineTitle
	start := bytes.Index(p, []byte(prefix))
	if start < 0 {
		return p
	}
	end := bytes.IndexByte(p[start+len(prefix):], '\a')
	if end < 0 {
		return p
	}
	end += start + len(prefix)
	drawFrame := frame != nil && string(p[start+len(titleOSC):end]) == frame.title &&
		!bytes.Contains(p, []byte(ansi.ResetModeAltScreenSaveCursor)) &&
		!bytes.Contains(p, []byte(ansi.ResetModeAltScreen))

	tag := ansi.SetWindowTitle("fndr")
	size := len(p) - (end + 1 - start) + len(tag)
	if drawFrame {
		size += len(frame.img.seq) + 24
	}
	buf := make([]byte, 0, size)
	buf = append(buf, p[:start]...)
	buf = append(buf, tag...)
	buf = append(buf, p[end+1:]...)
	if drawFrame {
		buf = append(buf, ansi.SaveCursor...)
		buf = append(buf, ansi.CursorPosition(frame.col, 3)...)
		buf = append(buf, frame.img.seq...)
		buf = append(buf, ansi.RestoreCursor...)
	}
	return buf
}
