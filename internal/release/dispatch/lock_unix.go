//go:build unix

package dispatch

import (
	"fmt"
	"os"
	"syscall"
)

// lockExclusive takes an exclusive lock on path, waiting for any holder, and returns its release.
//
// flock is held by the open file, so the kernel drops it when the holder exits, however it exits. A
// lock that a crashed build could leave behind would need staleness rules, and any staleness rule is a
// guess about how long a cold Swift build takes.
func lockExclusive(path string) (func(), error) {
	file, openError := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if openError != nil {
		return nil, fmt.Errorf("opening the lock %s: %w", path, openError)
	}
	if flockError := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); flockError != nil {
		file.Close()
		return nil, fmt.Errorf("taking the lock %s: %w", path, flockError)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}, nil
}
