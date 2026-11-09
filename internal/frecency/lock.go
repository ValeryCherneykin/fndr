package frecency

import (
	"os"
	"syscall"
)

// lock takes an exclusive lock on the file at path, creating it if needed,
// and waits while another fndr holds it. The lock lives in its own file
// because the history file itself is replaced on every save: a lock on it
// would stay with the old, renamed-over copy.
//
// flock is advisory: it only orders processes that ask for it, which is
// every fndr. The kernel drops it when the process dies, so a crash never
// leaves the history locked.
func lock(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil { //nolint:gosec // a file descriptor fits in an int
		f.Close() //nolint:errcheck,gosec // already failing
		return nil, err
	}
	return func() {
		// Closing the file releases the lock.
		f.Close() //nolint:errcheck,gosec // nothing was written to it
	}, nil
}
