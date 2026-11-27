//go:build !unix

package process_reading

import (
	"errors"
	"runtime"
)

// Niceness answers an error outside Unix, which has no nice value.
func Niceness(pid int) (int, error) {
	return 0, errors.New("a nice value is a Unix priority, and " + runtime.GOOS + " has none")
}
