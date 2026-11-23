package platform

import "syscall"

// Handle is a windows handle.
type Handle syscall.Handle

// Separator is the build's path separator.
const Separator = '\\'

// Open opens name for reading.
func Open(name string) (Handle, error) {
	handle, openError := syscall.Open(name, syscall.O_RDONLY, 0)
	return Handle(handle), openError
}
