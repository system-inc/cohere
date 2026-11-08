package memory_limit

import (
	"path"
	"strings"
)

// Limit is the smallest memory limit stated on a process's path to its hierarchy's root.
type Limit struct {
	// Bytes is the limit. It is zero when Source is NoLimit, which is no limit, not a limit of zero.
	Bytes uint64
	// Source is the hierarchy that stated Bytes, or NoLimit where no cgroup on the path states one.
	Source Source
	// Cgroup is the cgroup that stated Bytes, as /proc/self/cgroup prints a path ("/" for the root),
	// so a caller reads the usage the limit holds from there (UsageFile). It is empty for NoLimit.
	Cgroup string
}

// UsageFile is the fs.FS path of the usage file of the cgroup that stated the limit: memory.current
// on v2, memory.usage_in_bytes on v1. Its number counts every process under that cgroup, all of which
// the limit holds, where the process's own leaf would count only itself. It is empty for NoLimit.
func (limit Limit) UsageFile() string {
	switch limit.Source {
	case CgroupV2:
		return path.Join(unifiedMount, strings.TrimPrefix(limit.Cgroup, "/"), unifiedUsage)
	case CgroupV1:
		return path.Join(memoryV1Mount, strings.TrimPrefix(limit.Cgroup, "/"), memoryV1Usage)
	}
	return ""
}
