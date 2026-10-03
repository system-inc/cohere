//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// holdTableLock takes the table's lock exclusively, waiting for any other writer, and returns its release.
// It returns a release that does nothing when the lock cannot be taken, since writing without it is what
// every run did before it existed.
//
// flock belongs to the open file, so the kernel drops it when the holder exits however it exits, and a
// killed writer can never leave the next run waiting on a lock nobody holds.
func holdTableLock(tablePath string) func() {
	file, err := os.OpenFile(tableLockPath(tablePath), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return func() {}
	}
	return func() {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}
}

// waitForTableWriter returns once no run holds the table's lock, or after bound. It reports whether it
// returned because the writer finished (or there was none).
func waitForTableWriter(tablePath string, bound time.Duration) bool {
	file, err := os.Open(tableLockPath(tablePath))
	if err != nil {
		// No lock file, so no run has ever written after answering here.
		return true
	}
	defer file.Close()
	deadline := time.Now().Add(bound)
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
		if err == nil {
			syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			return true
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}
