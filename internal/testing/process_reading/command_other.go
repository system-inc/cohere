//go:build !darwin && !linux

package process_reading

import (
	"errors"
	"runtime"
)

// Command answers an error where no reader of a process's command line is written.
func Command(pid int) ([]string, error) {
	return nil, errors.New("reading a process's command line isn't written for " + runtime.GOOS)
}
