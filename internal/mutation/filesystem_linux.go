package mutation

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameExclusive(from, to string) error {
	return unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
}

func birthTime(path string, _ os.FileInfo, id FileID) int64 {
	var st unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_INO|unix.STATX_BTIME, &st); err != nil {
		return 0
	}
	if st.Mask&(unix.STATX_INO|unix.STATX_BTIME) != unix.STATX_INO|unix.STATX_BTIME ||
		st.Ino != id.Ino || unix.Mkdev(st.Dev_major, st.Dev_minor) != id.Dev {
		return 0
	}
	return st.Btime.Sec*1e9 + int64(st.Btime.Nsec)
}
