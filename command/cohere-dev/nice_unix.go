//go:build unix && !linux

package main

import (
	"errors"
	"syscall"
)

// lowerPriority makes this process, and so every go command and test binary it starts, nice the given
// amount. A process already nicer than that is left as it is: raising priority back takes privilege, so the
// kernel refuses it, and the refusal means there was nothing to do.
func lowerPriority(niceness int) error {
	setpriorityError := syscall.Setpriority(syscall.PRIO_PROCESS, 0, niceness)
	if errors.Is(setpriorityError, syscall.EACCES) || errors.Is(setpriorityError, syscall.EPERM) {
		return nil
	}
	return setpriorityError
}
