//go:build unix

package process_reading

import "syscall"

// Niceness is the nice value pid runs at, from -20 to 19, as nice and ps print it, read with getpriority.
func Niceness(pid int) (int, error) {
	priority, priorityError := syscall.Getpriority(syscall.PRIO_PROCESS, pid)
	if priorityError != nil {
		return 0, priorityError
	}
	return niceFromPriority(priority), nil
}
