package main

import "os"

// availableMemory reads /proc/meminfo and, inside a container, the cgroup's limit. See
// linuxAvailableMemory.
func availableMemory() (uint64, error) {
	meminfo, readError := os.ReadFile("/proc/meminfo")
	if readError != nil {
		return 0, readError
	}
	read := func(path string) []byte {
		// Ignored: a cgroup file that can't be read is a limit this container doesn't set, and
		// linuxAvailableMemory applies no limit for contents it can't parse, empty ones included.
		contents, _ := os.ReadFile(path)
		return contents
	}
	return linuxAvailableMemory(meminfo, linuxCgroupMemory{
		v2Maximum: read("/sys/fs/cgroup/memory.max"),
		v2Current: read("/sys/fs/cgroup/memory.current"),
		v1Limit:   read("/sys/fs/cgroup/memory/memory.limit_in_bytes"),
		v1Usage:   read("/sys/fs/cgroup/memory/memory.usage_in_bytes"),
	})
}
