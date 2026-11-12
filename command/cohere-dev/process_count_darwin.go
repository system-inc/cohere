package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// processNoun is what the process limit counts here.
const processNoun = "processes"

// backstopFactor is how many times the margin the account-wide backstop limit allows (bound.go): macOS counts
// processes against it, so four times the group's margin.
const backstopFactor = 4

// groupProcessCount is how many processes are in a process group, read from the kernel by group id.
func groupProcessCount(group int) (int, error) {
	processes, sysctlError := unix.SysctlKinfoProcSlice("kern.proc.pgrp", group)
	if sysctlError != nil {
		return 0, sysctlError
	}
	return len(processes), nil
}

// accountProcessCount is how many processes this account owns, by real user id, which is what the kernel
// counts against the process limit when one forks.
func accountProcessCount() (int, error) {
	processes, sysctlError := unix.SysctlKinfoProcSlice("kern.proc.ruid", os.Getuid())
	if sysctlError != nil {
		return 0, sysctlError
	}
	return len(processes), nil
}
