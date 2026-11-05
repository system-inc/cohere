//go:build darwin || linux

package program

import (
	"os"
	"syscall"
)

// mapFile maps a file read-only for the life of the process, or returns nil. Nothing unmaps it: the pack
// serves strings over the mapping, and the compiler and the rules keep substrings of those as long as the
// process runs. See the content pack's package comment.
func mapFile(path string) []byte {
	file, openError := os.Open(path)
	if openError != nil {
		return nil
	}
	// Ignored: the file is opened read-only, only to map it, so closing it loses no write, and the mapping
	// outlives the descriptor.
	defer file.Close()
	information, statError := file.Stat()
	if statError != nil || information.Size() == 0 {
		return nil
	}
	mapped, memoryMapError := syscall.Mmap(int(file.Fd()), 0, int(information.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if memoryMapError != nil {
		return nil
	}
	return mapped
}
