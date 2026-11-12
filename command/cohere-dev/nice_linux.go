package main

import (
	"errors"
	"os"
	"strconv"
	"syscall"
)

// lowerPriority makes this process, and so every go command and test binary it starts, nice the given amount. On
// Linux a process's niceness is each thread's own, and setpriority on the process sets only the calling thread's, so
// a child Go started from another thread ran at nice 0 (TestARunIsNiceUnlessItAsksForFullPriority on Home, Nov 12,
// #acdf5mc). So every thread is niced, read from /proc/self/task, until a pass finds none it hadn't: a thread made
// later takes its maker's niceness. A thread already nicer is left as it is: raising priority back takes privilege,
// so the kernel refuses it, and the refusal means there was nothing to do.
func lowerPriority(niceness int) error {
	niced := map[int]bool{}
	for {
		tasks, readDirectoryError := os.ReadDir("/proc/self/task")
		if readDirectoryError != nil {
			return readDirectoryError
		}
		added := false
		for _, task := range tasks {
			thread, parseError := strconv.Atoi(task.Name())
			if parseError != nil || niced[thread] {
				continue
			}
			setpriorityError := syscall.Setpriority(syscall.PRIO_PROCESS, thread, niceness)
			// `ESRCH` is a thread that ended since the listing, which nothing starts from now.
			if setpriorityError != nil && !errors.Is(setpriorityError, syscall.EACCES) &&
				!errors.Is(setpriorityError, syscall.EPERM) && !errors.Is(setpriorityError, syscall.ESRCH) {
				return setpriorityError
			}
			niced[thread] = true
			added = true
		}
		if !added {
			return nil
		}
	}
}
