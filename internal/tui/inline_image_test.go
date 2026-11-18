package tui

import (
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestInlineImagePayloads(t *testing.T) {
	path := pngFile(t, t.TempDir(), 40, 20)
	b := box{cols: 10, rows: 5, cellWidth: 8, cellHeight: 16}
	p, err := buildInlineImage(path, 16, b, graphicsITerm)
	if err != nil {
		t.Fatal(err)
	}
	header, payload, ok := strings.Cut(strings.TrimSuffix(p.seq, "\a"), ":")
	if !ok || !strings.HasPrefix(header, "\x1b]1337;File=") || !strings.Contains(header, "inline=1") {
		t.Fatalf("invalid iTerm2 header %q", header)
	}
	img, err := png.Decode(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)))
	if err != nil || img.Bounds().Dx() != 40 || img.Bounds().Dy() != 20 {
		t.Fatalf("iTerm2 PNG: %v", err)
	}

	p, err = buildInlineImage(path, 16, b, graphicsSixel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.seq, "\x1bP0;1q\"1;1;80;40") || !strings.HasSuffix(p.seq, "\x1b\\") {
		t.Fatal("Sixel payload lacks the 80×40 raster or framing")
	}
	if p.cols != 10 || p.rows != 3 {
		t.Fatalf("Sixel area = %d×%d, want 10×3", p.cols, p.rows)
	}
}

func TestSixelNeedsDeviceCapabilityAndCellSize(t *testing.T) {
	m := testModel(t)
	m.probeGraphics = true
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	if m.graphicsProbed {
		t.Fatal("queried while preview was hidden")
	}
	m.preview = true
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	msg, ok := cmd().(tea.RawMsg)
	if !ok || msg.Msg != ansi.RequestPrimaryDeviceAttributes {
		t.Fatalf("probe = %#v", msg)
	}
	m.Update(uv.PrimaryDeviceAttributesEvent{65, 1})
	if m.graphics != graphicsNone {
		t.Fatal("enabled Sixel without capability 4")
	}
	m.Update(uv.PrimaryDeviceAttributesEvent{65, 1, 4})
	if m.graphics != graphicsSixel || m.imageBox().cellWidth != 0 {
		t.Fatal("Sixel did not wait for cell geometry")
	}
	m.Update(uv.CellSizeEvent{Width: 9, Height: 19})
	m.Update(uv.PixelSizeEvent{Width: 960, Height: 320})
	if b := m.imageBox(); b.cellWidth != 9 || b.cellHeight != 19 {
		t.Fatalf("cell size = %d×%d, want 9×19", b.cellWidth, b.cellHeight)
	}
}

func TestInlineOutputOnlyDrawsTheCurrentFrame(t *testing.T) {
	f := &inlineFrame{title: inlineTitle + "2", col: 63, img: &paneImage{seq: "image"}}
	raw := ansi.SetWindowTitle(f.title) + "frame"
	if got := string(inlineFrameOutput([]byte(raw), f)); got != ansi.SetWindowTitle("fndr")+"frame"+
		ansi.SaveCursor+ansi.CursorPosition(63, 3)+"image"+ansi.RestoreCursor {
		t.Fatalf("frame = %q", got)
	}
	if got := string(inlineFrameOutput([]byte(ansi.SetWindowTitle(inlineTitle+"1")+"old frame"), f)); got != ansi.SetWindowTitle("fndr")+"old frame" {
		t.Fatal("painted over a stale frame")
	}
	raw += ansi.ResetModeAltScreenSaveCursor
	if got := string(inlineFrameOutput([]byte(raw), f)); got != ansi.SetWindowTitle("fndr")+"frame"+ansi.ResetModeAltScreenSaveCursor {
		t.Fatal("painted after leaving the alternate screen")
	}
}
