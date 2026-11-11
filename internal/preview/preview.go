// Package preview gathers what the preview pane shows for a path: the
// contents of a folder, the first lines of a text file, or a card of facts
// about anything else. It returns data, not styled text; drawing it is the
// TUI's job.
//
// Build reads at most readLen bytes of a file, so previewing a 10 GB log
// costs the same as previewing a 1 KB note.
package preview

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/ValeryCherneykin/fndr/internal/indexer"
	"github.com/ValeryCherneykin/fndr/internal/opener"
)

// readLen is how much of a file is read for its preview: enough lines to
// fill a tall terminal, bounded so huge files cost nothing extra.
const readLen = 32 << 10

// Kind says which of the preview's fields are filled in.
type Kind int

// Kinds of preview.
const (
	// Card is a file with nothing readable to show: Type, Size, ModTime.
	Card Kind = iota
	// Text is a text file: Lines holds its first lines.
	Text
	// Dir is a folder: Entries holds its contents in display order.
	Dir
	// Image is a picture LoadImage can decode. Type, Size and ModTime are
	// filled in as for a Card, for terminals that cannot show pictures.
	Image
)

// Preview is what the preview pane shows for one path.
type Preview struct {
	Kind    Kind
	Lines   []string        // Text: up to maxLines lines, safe to print
	Entries []indexer.Entry // Dir: the folder's contents
	Type    string          // Card: what the file is, such as "PDF document"
	Size    int64
	ModTime time.Time
}

// Build gathers the preview of path, keeping at most maxLines lines of
// text.
func Build(path string, maxLines int) (Preview, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Preview{}, fmt.Errorf("preview: %w", err)
	}
	p := Preview{Size: info.Size(), ModTime: info.ModTime()}

	if info.IsDir() {
		p.Kind = Dir
		p.Entries, err = indexer.List(path)
		return p, err
	}

	if !info.Mode().IsRegular() {
		// A pipe, socket or device: reading it could wait forever, or read
		// the keys meant for fndr (/dev/tty). What it is says enough.
		p.Type = special(info.Mode())
		return p, nil
	}

	head, err := readHead(path)
	if err != nil {
		return Preview{}, fmt.Errorf("preview: %w", err)
	}
	switch opener.ClassifyHead(head) {
	case opener.Text:
		p.Kind = Text
		p.Lines = lines(head, maxLines)
	case opener.Program:
		p.Type = "program"
	default:
		p.Type = typeName(head)
		if decodable[p.Type] {
			p.Kind = Image
		}
	}
	return p, nil
}

// special names a file that is not a regular one, for its card.
func special(mode os.FileMode) string {
	switch {
	case mode&os.ModeNamedPipe != 0:
		return "named pipe"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeDevice != 0:
		return "device"
	}
	return "special file"
}

// readHead reads up to readLen bytes from the start of path.
func readHead(path string) ([]byte, error) {
	f, err := opener.OpenFile(path) // a file swapped for a pipe since the Stat is refused, not waited on
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only: Close has nothing to report

	buf := make([]byte, readLen)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

// lines splits head into at most limit lines, made safe to print.
func lines(head []byte, limit int) []string {
	text := strings.ReplaceAll(string(head), "\r\n", "\n")
	out := strings.SplitN(text, "\n", limit+1)
	if len(out) > limit {
		out = out[:limit]
	}
	for i, l := range out {
		out[i] = sanitize(l)
	}
	return out
}

// sanitize replaces control characters with "·", keeping tabs. A file is
// data, and data must never drive the terminal: an escape sequence printed
// as-is could clear the screen, move the cursor or retitle the window.
// Bidi overrides go too: they reorder the line on screen, the "Trojan
// Source" trick that shows code other than what the file says.
func sanitize(s string) string {
	if !strings.ContainsFunc(s, isUnsafe) {
		return s // the common case: no copy
	}
	return strings.Map(func(r rune) rune {
		if isUnsafe(r) {
			return '·'
		}
		return r
	}, s)
}

func isUnsafe(r rune) bool {
	return r != '\t' && unicode.IsControl(r) || 0x202a <= r && r <= 0x202e || 0x2066 <= r && r <= 0x2069
}
