//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// tableLockPath is the lock in a cache table's directory.
func tableLockPath(directory string) string {
	return filepath.Join(directory, "table.lock")
}

// holdTableLock takes the table's lock exclusively, waiting for any other writer, and returns its release.
// It returns a release that does nothing when the lock cannot be taken, since writing without it is what
// every run did before it existed.
//
// flock belongs to the open file, so the kernel drops it when the holder exits however it exits, and a
// killed writer can never leave the next run waiting on a lock nobody holds.
func holdTableLock(directory string) func() {
	file, openError := os.OpenFile(tableLockPath(directory), os.O_CREATE|os.O_RDWR, 0o644)
	if openError != nil {
		return func() { /* No lock was taken, so there is nothing to release. */ }
	}
	if flockError := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); flockError != nil {
		_ = file.Close() // Ignored: never written, and never locked.
		return func() { /* No lock was taken, so there is nothing to release. */ }
	}
	return func() {
		// Ignored: the lock belongs to the open file, so the close below drops it whether or not the unlock took,
		// and the process's exit drops it either way.
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close() // Ignored: nothing was written through it, and the exit closes it either way.
	}
}

// holdTableReadLock takes the table's lock shared, waiting up to bound for a writer to finish, and returns
// its release and whether it was taken. Past bound it gives up and returns a release that does nothing, and
// the run reads without it, where the worst outcome is a miss, never a stale hit.
//
// The lock file is created here as a writer would, so a reader racing the very first write in a directory
// holds the same lock that write is about to take, rather than finding no file and reading around it. With no
// directory at all there is nothing to read, and no lock is taken.
func holdTableReadLock(directory string, bound time.Duration) (func(), bool) {
	file, openError := os.OpenFile(tableLockPath(directory), os.O_CREATE|os.O_RDONLY, 0o644)
	if openError != nil {
		return func() { /* No lock was taken, so there is nothing to release. */ }, true
	}
	deadline := time.Now().Add(bound)
	for {
		flockError := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
		if flockError == nil {
			return func() {
				// Ignored: as holdTableLock's release.
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close() // Ignored: as holdTableLock's release.
			}, true
		}
		if !errors.Is(flockError, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			_ = file.Close() // Ignored: opened only to lock, never written, and never locked.
			return func() { /* No lock was taken, so there is nothing to release. */ }, false
		}
		time.Sleep(2 * time.Millisecond)
	}
}
