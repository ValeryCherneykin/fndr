package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// A file name is data from the disk, and anyone can choose it: a downloaded
// archive can hold a file named "\x1b]52;c;...\a", which a terminal reads as
// "put this in the clipboard", or a name with a newline that breaks the
// screen apart. Names, and messages that quote them, must never drive the
// terminal, so everything that comes from the disk goes through safeText on
// its way to the screen. Only the display is cleaned: paths themselves stay
// exact, or the file could not be found again.

// safeText replaces what a terminal could act on with visible stand-ins:
//
//	control characters (C0, DEL) and invalid UTF-8 bytes   ?
//	C1 control characters (U+0080-U+009F)                   ·
//	bidirectional overrides (U+202A-U+202E, U+2066-U+2069)  �
//
// The bidi overrides do not command the terminal but reorder what it shows:
// "invoice\u202Efdp.exe" reads as "invoiceexe.pdf".
//
// Every stand-in has the byte length of what it replaces, so byte offsets
// into the original, such as the match positions of a search result, are
// still right in the result.
func safeText(s string) string {
	if isPlainASCII(s) {
		return s // nearly every name: no copy
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1, r < 0x20, r == 0x7f:
			b.WriteByte('?')
		case 0x80 <= r && r <= 0x9f:
			b.WriteString("·") // 2 bytes, as is r
		case isBidi(r):
			b.WriteRune(utf8.RuneError) // 3 bytes, as is r
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// isPlainASCII reports whether s is printable ASCII only, which is safe as
// it is.
func isPlainASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

// isBidi reports whether r is one of the invisible characters that reverse
// or isolate the direction of the text around them.
func isBidi(r rune) bool {
	return 0x202a <= r && r <= 0x202e || 0x2066 <= r && r <= 0x2069
}

// dropControls removes control characters and bidi overrides from pasted
// text, which is printed on the query line and so must be safe as it goes
// in. Whitespace controls (tab, newline) are kept for the caller to fold
// into spaces.
func dropControls(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) || isBidi(r) {
			return -1
		}
		return r
	}, s)
}
