// Package filelock provides advisory file locking for coordinating
// concurrent access to shared resources (e.g., config files).
package filelock

import "os"

const lockFileMode = 0o600

// Lock acquires an exclusive advisory lock on the file at path,
// creating it if it does not exist. The returned function releases
// the lock and must be called when the critical section is done.
//
// Only one process can hold the lock at a time; other callers block
// until the lock is available.
func Lock(path string) (unlock func() error, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, lockFileMode) //nolint:gosec // lock file path from trusted source
	if err != nil {
		return nil, err
	}

	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}

	return func() error {
		unlockErr := unlockFile(f)
		closeErr := f.Close()
		if unlockErr != nil {
			return unlockErr
		}
		return closeErr
	}, nil
}
