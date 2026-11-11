//go:build darwin || linux

package main

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

// ownGroup starts a command in a process group of its own, so its whole group can be signalled.
func ownGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup sends a signal to every process in a group.
func signalGroup(group int, signal syscall.Signal) {
	// Ignored: the one failure that can happen is a group already empty, which is what the signal is for.
	_ = syscall.Kill(-group, signal)
}

// groupAlive reports whether any process is still in a group.
func groupAlive(group int) bool {
	killError := syscall.Kill(-group, 0)
	// `EPERM` is a process in the group this account may not signal, which is still a process in it.
	return killError == nil || errors.Is(killError, syscall.EPERM)
}

// holdProcessLimit holds this process, and everything it starts, to a process limit, soft and hard, so nothing
// beneath it can raise it again. A hard limit already lower stands, since raising one takes privilege.
func holdProcessLimit(limit int) error {
	var current unix.Rlimit
	if getError := unix.Getrlimit(unix.RLIMIT_NPROC, &current); getError != nil {
		return getError
	}
	held := min(uint64(limit), current.Max)
	return unix.Setrlimit(unix.RLIMIT_NPROC, &unix.Rlimit{Cur: held, Max: held})
}
