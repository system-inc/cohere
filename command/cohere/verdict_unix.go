//go:build !windows

package main

import (
	"os"
	"strconv"
	"syscall"
)

func takeVerdictFile() *os.File {
	value := os.Getenv(VerdictVariable)
	if value == "" {
		return nil
	}
	os.Unsetenv(VerdictVariable)
	descriptor, err := strconv.Atoi(value)
	if err != nil || descriptor < 3 {
		return nil
	}
	syscall.CloseOnExec(descriptor)
	return os.NewFile(uintptr(descriptor), "verdict")
}
