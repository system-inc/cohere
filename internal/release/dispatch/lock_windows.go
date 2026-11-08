//go:build windows

package dispatch

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// wholeFile is the byte count LockFileEx takes in each of its two halves to lock every byte the file could hold,
// as cmd/go's lockedfile does.
const wholeFile = ^uint32(0)

// lockExclusive takes an exclusive lock on path, waiting for any holder, and returns its release.
//
// LockFileEx is held by the open handle, as flock is held by the open file on Unix, so Windows drops it when the
// holder exits, however it exits. A lock that a crashed build could leave behind would need staleness rules, and
// any staleness rule is a guess about how long a cold Swift build takes.
func lockExclusive(path string) (func(), error) {
	file, openError := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if openError != nil {
		return nil, fmt.Errorf("opening the lock %s: %w", path, openError)
	}
	locked := new(windows.Overlapped)
	if lockError := windows.LockFileEx(
		windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, wholeFile, wholeFile, locked,
	); lockError != nil {
		// Ignored: the lock was never taken and its error is the one returned, and the file holds nothing written.
		file.Close()
		return nil, fmt.Errorf("taking the lock %s: %w", path, lockError)
	}
	return func() {
		// Ignored: closing the file below drops the lock whatever the unlock answers.
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, wholeFile, wholeFile, locked)
		// Ignored: the file is only the lock's handle and holds nothing written, and Windows drops the lock when
		// this process exits if the close did not.
		file.Close()
	}, nil
}
