package mutation

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Trash moves files and folders to the user's trash instead of deleting
// them: fndr never removes anything for good.
//
// A trash is only a folder, and putting something in it is a rename: as
// fast for a folder of a million files as for one file, and undone by
// moving it back. macOS keeps the trash in ~/.Trash; Linux desktops follow
// the freedesktop.org spec, ~/.local/share/Trash, where every item gets an
// info file saying where it came from, so the file manager can put it back.
type Trash struct {
	files string // where trashed items go
	info  string // freedesktop: where their .trashinfo files go; "" on macOS
}

// ErrOtherVolume means the item is on another disk than the trash. Moving it
// would mean copying it, and deleting it is not on offer, so it stays.
var ErrOtherVolume = errors.New("on another disk than the trash")

// HomeTrash returns the trash of the current user.
func HomeTrash() (*Trash, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("trash: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return &Trash{files: filepath.Join(home, ".Trash")}, nil
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" || !filepath.IsAbs(data) {
		data = filepath.Join(home, ".local", "share")
	}
	return freedesktop(filepath.Join(data, "Trash")), nil
}

func freedesktop(dir string) *Trash {
	return &Trash{files: filepath.Join(dir, "files"), info: filepath.Join(dir, "info")}
}

// Put moves path into the trash and returns where it went. A name already
// in the trash is never replaced: the newcomer gets " 2", " 3", and so on.
func (t *Trash) Put(path string) (string, error) {
	if err := os.MkdirAll(t.files, 0o700); err != nil {
		return "", fmt.Errorf("trash: %w", err)
	}
	if t.info != "" {
		if err := os.MkdirAll(t.info, 0o700); err != nil {
			return "", fmt.Errorf("trash: %w", err)
		}
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext == base { // ".env" is a name, not an extension
		ext = ""
	}
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 10000; i++ {
		name := base
		if i > 1 {
			name = stem + " " + strconv.Itoa(i) + ext
		}
		dst, err := t.put(path, name)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("trash %s: %w", base, err)
		}
		return dst, nil
	}
	return "", fmt.Errorf("trash %s: no free name", base)
}

// put tries to trash path under name, returning fs.ErrExist if the name is
// taken.
func (t *Trash) put(path, name string) (string, error) {
	dst := filepath.Join(t.files, name)
	var info string
	if t.info != "" {
		// The spec makes the info file the claim on the name: created with
		// O_EXCL, it is atomic even with another program trashing at once.
		info = filepath.Join(t.info, name+".trashinfo")
		f, err := os.OpenFile(info, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return "", err
		}
		_, err = fmt.Fprintf(f, "[Trash Info]\nPath=%s\nDeletionDate=%s\n",
			(&url.URL{Path: path}).EscapedPath(), time.Now().Format("2006-01-02T15:04:05"))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(info) //nolint:errcheck // already failing; the info file is ours
			return "", err
		}
	}
	// rename would replace a file at dst; check first. A file appearing
	// in between is not ours to worry about: nobody else writes here under
	// a name we have just found free, and the info file claims it on Linux.
	if _, err := os.Lstat(dst); err == nil {
		if info != "" {
			_ = os.Remove(info) //nolint:errcheck // releasing our own claim
		}
		return "", fs.ErrExist
	}
	if err := os.Rename(path, dst); err != nil {
		if info != "" {
			_ = os.Remove(info) //nolint:errcheck // releasing our own claim
		}
		if errors.Is(err, syscall.EXDEV) {
			return "", ErrOtherVolume
		}
		return "", err
	}
	return dst, nil
}
