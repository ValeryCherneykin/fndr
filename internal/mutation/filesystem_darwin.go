package mutation

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func renameExclusive(from, to string) error {
	return unix.RenamexNp(from, to, unix.RENAME_EXCL)
}

func birthTime(_ string, info os.FileInfo, _ FileID) int64 {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return st.Birthtimespec.Nano()
}
