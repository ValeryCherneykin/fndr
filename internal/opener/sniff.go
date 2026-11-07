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

// IsText reports whether the file at path looks like text, judging by its
// first sniffLen bytes. Extensions are not consulted: they miss Makefile,
// .zshrc and every extension nobody listed, and they lie.
func IsText(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close() //nolint:errcheck // read-only: Close has nothing to report

	buf := make([]byte, sniffLen)
	n, err := io.ReadFull(f, buf)
	// A file shorter than sniffLen is the common case, not an error.
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}
	return looksLikeText(buf[:n]), nil
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
