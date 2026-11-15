// Package journal keeps records on disk so that what fndr was doing
// outlives fndr: a closed window, a crash, kill -9. A record is appended
// whole or not at all, and is on the disk when Append returns, so a step
// written down before it is taken is known after any crash; that is the
// write-ahead log of a database, and it is what lets fndr finish or undo
// a batch it was cut off in.
//
// The package knows nothing of files or plans: a record is bytes, framed
// and checked. What they mean is the caller's.
//
// The format is a header line, then records:
//
//	fndr-journal 1\n
//	[length uint32][crc32c uint32][length bytes]   big-endian, crc of the bytes
//	...
//
// A crash in the middle of an append leaves a torn record at the end:
// short, or with bytes the checksum does not cover. The first record that
// does not check out ends the journal, as in LevelDB's log: past it the
// framing cannot be trusted, so whatever follows is the torn tail, not
// read, and cut off by the next Append before it writes, so that a new
// record never follows garbage.
package journal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// header starts every journal: a file without it is not one, and is never
// cut or written to.
const header = "fndr-journal 1\n"

// frame is the bytes before a record's own: its length and checksum.
const frame = 8

// MaxRecord is the largest record Append takes: a journal holds plans, not
// files, and a record this large is a caller's mistake.
const MaxRecord = 16 << 20

// ErrNotJournal is a file at the path that is not a journal.
var ErrNotJournal = errors.New("not a fndr journal")

// ErrTooLarge is a record longer than MaxRecord.
var ErrTooLarge = errors.New("journal record too large")

// castagnoli is CRC-32C, the checksum of iSCSI, ext4 and LevelDB: better at
// catching errors than the IEEE one, and done by the CPU on arm64 and amd64.
var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Append adds rec as one record at the end of the journal at path, made if
// it is not there (0600: the records may name the user's files), and
// returns once the record is on the disk. A torn tail left by a crash is
// cut off first. Appends of several fndr at once are taken one at a time.
func Append(path string, rec []byte) error {
	if len(rec) > MaxRecord {
		return ErrTooLarge
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // synced before, or failing already
	// flock is per open file, dropped by the kernel when fndr dies: a crash
	// never leaves the journal locked.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil { //nolint:gosec // a file descriptor fits in an int
		return err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	_, end, err := parse(data)
	if err != nil {
		return err
	}
	if end < len(data) {
		if err := f.Truncate(int64(end)); err != nil {
			return err
		}
	}

	var buf bytes.Buffer
	if end == 0 {
		// No header yet, or one torn by a crash on the first append: it
		// goes with this record. The folder is synced too, or a crash
		// could lose the new file's name itself.
		buf.WriteString(header)
		defer syncDir(filepath.Dir(path)) //nolint:errcheck // the record is safe; at worst its name is not yet
	}
	var head [frame]byte
	binary.BigEndian.PutUint32(head[:4], uint32(len(rec))) //nolint:gosec // at most MaxRecord
	binary.BigEndian.PutUint32(head[4:], crc32.Checksum(rec, castagnoli))
	buf.Write(head[:])
	buf.Write(rec)
	if _, err := f.WriteAt(buf.Bytes(), int64(end)); err != nil {
		return err
	}
	// The point of the journal, and the one line no test can check: only
	// power cut at the wrong moment tells a synced record from one in the
	// page cache. On macOS Go's Sync is F_FULLFSYNC, down to the platter.
	return f.Sync()
}

// Read returns the records of the journal at path, in the order they were
// appended: none if there is no journal. A torn tail is not an error; it is
// left out.
func Read(path string) ([][]byte, error) {
	f, err := os.Open(path) //nolint:gosec // fndr's own journal
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read only
	// Shared: no append is halfway through what is read.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil { //nolint:gosec // as in Append
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil || len(data) == 0 {
		return nil, err
	}
	recs, _, err := parse(data)
	return recs, err
}

// parse returns the whole records of a journal's bytes, and where they end:
// past it is a torn tail, or nothing. end is 0 for no journal yet: no
// bytes, or a header torn by a crash on the first append. The records
// share data's array.
func parse(data []byte) (recs [][]byte, end int, err error) {
	if !bytes.HasPrefix(data, []byte(header)) {
		if len(data) < len(header) && bytes.HasPrefix([]byte(header), data) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("%w: no header", ErrNotJournal)
	}
	end = len(header)
	for {
		rest := data[end:]
		if len(rest) < frame {
			return recs, end, nil
		}
		n := binary.BigEndian.Uint32(rest[:4])
		if int(n) > len(rest)-frame { // a damaged length too: no allocation, rest is sliced
			return recs, end, nil
		}
		rec := rest[frame : frame+int(n) : frame+int(n)]
		if crc32.Checksum(rec, castagnoli) != binary.BigEndian.Uint32(rest[4:frame]) {
			return recs, end, nil
		}
		recs = append(recs, rec)
		end += frame + int(n)
	}
}

// syncDir makes the names in dir durable: a new file's included.
func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // the journal's own folder
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck // read only
	return d.Sync()
}
