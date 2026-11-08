package main

import "os"

// availableMemory reads this host's root. See linuxAvailableMemoryFrom.
func availableMemory() (uint64, error) {
	return linuxAvailableMemoryFrom(os.DirFS("/"))
}
