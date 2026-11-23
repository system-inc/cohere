//go:build unix

package platform

import "syscall"

// Handle is a file descriptor.
type Handle int

// Separator is the build's path separator.
const Separator = '/'

// Open opens name for reading.
func Open(name string) (Handle, error) {
	descriptor, openError := syscall.Open(name, syscall.O_RDONLY, 0)
	return Handle(descriptor), openError
}
