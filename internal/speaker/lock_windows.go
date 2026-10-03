package speaker

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

var errLocked = errors.New("locked by another sptui")

// lock takes an exclusive lock on path, held until unlock or exit.
func lock(path string) (func(), error) {
	return lockFile(path, windows.LOCKFILE_FAIL_IMMEDIATELY)
}

// lockWait is lock, waiting for the lock to be free.
func lockWait(path string) (func(), error) {
	return lockFile(path, 0)
}

func lockFile(path string, flags uint32) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	// Lock the first byte, whether or not the file has one.
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|flags, 0, 1, 0, new(windows.Overlapped))
	if err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errLocked
		}
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
