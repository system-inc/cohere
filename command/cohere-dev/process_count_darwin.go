package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// processNoun is what the process limit counts here.
const processNoun = "processes"

// accountProcessCount is how many processes this account owns, by real user id, which is what the kernel
// counts against the process limit when one forks.
func accountProcessCount() (int, error) {
	processes, sysctlError := unix.SysctlKinfoProcSlice("kern.proc.ruid", os.Getuid())
	if sysctlError != nil {
		return 0, sysctlError
	}
	return len(processes), nil
}
