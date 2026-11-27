//go:build !darwin && !linux

package process_reading

import (
	"errors"
	"runtime"
)

// Describe answers an error where no reader of a process's parent and start is written.
func Describe(pid int) (Description, error) {
	return Description{}, errors.New("describing a process isn't written for " + runtime.GOOS)
}
