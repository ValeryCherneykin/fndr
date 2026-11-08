package opener

import (
	"bytes"
	"errors"
	"io"
	"os"
	"unicode/utf8"
)

// sniffLen is how much of a file is read to tell text from binary. The same
// heuristic git and grep use; 8 KiB is enough to meet a NUL byte in any
// common binary format, and reading it costs one syscall.
const sniffLen = 8 << 10

// Kind is what a file is, as far as opening it is concerned.
type Kind int

// Kinds of file.
const (
	// Other files go to the system's default application: PDFs, images,
	// videos.
	Other Kind = iota
	// Text goes to the editor: code, notes, configs, scripts.
	Text
	// Program is a native executable. It is never opened: handing it to
	// the system would run it, and pressing enter on a search result must
	// never run anything.
	Program
)

// Classify reports what kind of file path is, judging by its first
// sniffLen bytes. Extensions are not consulted: they miss Makefile, .zshrc
// and every extension nobody listed, and they lie. Neither is the
// executable bit: files on FAT and exFAT drives all have it.
func Classify(path string) (Kind, error) {
	f, err := os.Open(path)
	if err != nil {
		return Other, err
	}
	defer f.Close() //nolint:errcheck // read-only: Close has nothing to report

	buf := make([]byte, sniffLen)
	n, err := io.ReadFull(f, buf)
	// A file shorter than sniffLen is the common case, not an error.
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return Other, err
	}
	return ClassifyHead(buf[:n]), nil
}

// ClassifyHead is Classify for a file whose start the caller has already
// read, such as the previewer, which reads more than sniffLen anyway.
func ClassifyHead(head []byte) Kind {
	switch {
	case looksLikeProgram(head):
		return Program
	case looksLikeText(head):
		return Text
	default:
		return Other
	}
}

// programMagic are the signatures native executables start with.
var programMagic = [][]byte{
	[]byte("\x7fELF"),          // Linux and other Unix
	[]byte("\xcf\xfa\xed\xfe"), // macOS, 64-bit (Mach-O)
	[]byte("\xce\xfa\xed\xfe"), // macOS, 32-bit
	// macOS universal binary (Intel + Apple Silicon). Java class files
	// share this signature; they do not run on a double-click either.
	[]byte("\xca\xfe\xba\xbe"),
}

// looksLikeProgram reports whether head starts with one of programMagic.
func looksLikeProgram(head []byte) bool {
	for _, magic := range programMagic {
		if bytes.HasPrefix(head, magic) {
			return true
		}
	}
	return false
}

// looksLikeText reports whether head, the first bytes of a file, looks like
// text an editor can open: valid UTF-8 without NUL bytes.
//
// head may be cut at sniffLen in the middle of a multi-byte character. That
// cut is not a sign of a binary file and must not make it one.
func looksLikeText(head []byte) bool {
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}

	// Drop a character cut short at the end of head, if any. Walk back to the
	// start byte of the last character (at most utf8.UTFMax bytes away) and
	// ask whether the bytes from there form a whole one. A stray invalid byte
	// such as 0xff counts as "whole" to FullRune, so real garbage is kept and
	// still fails the check below.
	for i := len(head) - 1; i >= 0 && i >= len(head)-utf8.UTFMax; i-- {
		if utf8.RuneStart(head[i]) {
			if !utf8.FullRune(head[i:]) {
				head = head[:i]
			}
			break
		}
	}
	return utf8.Valid(head)
}
