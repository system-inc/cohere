package memory_limit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Membership is what /proc/<pid>/cgroup says of a process: the path of its cgroup in the unified (v2)
// hierarchy, and in the v1 hierarchy that holds the memory controller, each with whether the file had
// that line at all. A path is as the kernel prints it, absolute within the hierarchy ("/" for its root,
// or for a cgroup namespace's root).
type Membership struct {
	Unified    string
	HasUnified bool
	Memory     string
	HasMemory  bool
}

// ParseMembership reads a /proc/<pid>/cgroup file: one "<id>:<controllers>:<path>" line per hierarchy.
// The unified hierarchy's line is "0::<path>", always, by the kernel's cgroup-v2 document; a v1 line
// names its controllers separated by commas, and the one naming memory is the one this module reads.
// Named v1 hierarchies ("1:name=systemd:/...") hold no controller and are passed over. A line that
// isn't three fields with a number first, or a second line for the same hierarchy, is refused, since a
// file the kernel didn't write would send the walk to some other cgroup's limit. The path is
// everything after the second colon, so a colon in a cgroup's name stays in its path.
func ParseMembership(contents []byte) (Membership, error) {
	var membership Membership
	for line := range strings.Lines(string(contents)) {
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 {
			return Membership{}, fmt.Errorf("/proc/self/cgroup: the line %q isn't \"<id>:<controllers>:<path>\"", line)
		}
		if _, parseError := strconv.ParseUint(fields[0], 10, 32); parseError != nil {
			return Membership{}, fmt.Errorf("/proc/self/cgroup: the line %q has no hierarchy number first", line)
		}
		switch {
		case fields[0] == "0" && fields[1] == "":
			if membership.HasUnified {
				return Membership{}, fmt.Errorf("/proc/self/cgroup: a second unified line, %q", line)
			}
			membership.Unified, membership.HasUnified = fields[2], true
		case slices.Contains(strings.Split(fields[1], ","), "memory"):
			if membership.HasMemory {
				return Membership{}, fmt.Errorf("/proc/self/cgroup: a second line naming memory, %q", line)
			}
			membership.Memory, membership.HasMemory = fields[2], true
		}
	}
	return membership, nil
}

// unlimitedFloor is where a limit file's number stops being a limit. cgroup v1 writes "no limit" as the
// largest signed 64-bit number rounded down to a page, 9223372036854771712 with 4 KiB pages and
// 9223372036854710272 with 64 KiB ones (the kernel's documents don't print it; read from hosts), and
// 1<<60 bytes, an exbibyte, is below both and above any machine's memory.
const unlimitedFloor = 1 << 60

// ParseLimit reads a limit file's contents, memory.max (v2) or memory.limit_in_bytes (v1). "max", an
// empty file and v1's unlimited number state no limit; whitespace around the number is the kernel's
// newline. Zero is a limit: a cgroup that may hold nothing. Anything else is an error, never read as no
// limit, since a limit that can't be read is not a limit that isn't there.
func ParseLimit(contents []byte) (bytes uint64, stated bool, limitError error) {
	text := strings.TrimSpace(string(contents))
	if text == "" || text == "max" {
		return 0, false, nil
	}
	number, numberError := strconv.ParseUint(text, 10, 64)
	if numberError != nil {
		return 0, false, fmt.Errorf("%q is neither a number of bytes nor \"max\"", text)
	}
	if number >= unlimitedFloor {
		return 0, false, nil
	}
	return number, true, nil
}

// Ancestry is a cgroup's path and every directory above it, nearest first, each relative to the
// hierarchy's root the way an fs.FS names a path: "/a/b" is ["a/b", "a", "."], and "/" is ["."]. A path
// that isn't absolute, or holds an empty, "." or ".." element, is refused. The kernel prints ".." where
// a process sits outside the reader's cgroup namespace, and walking that would read a cgroup the mount
// doesn't show.
func Ancestry(path string) ([]string, error) {
	rest, absolute := strings.CutPrefix(path, "/")
	if !absolute {
		return nil, fmt.Errorf("the cgroup %q isn't an absolute path in its hierarchy", path)
	}
	if rest == "" {
		return []string{"."}, nil
	}
	elements := strings.Split(rest, "/")
	for _, element := range elements {
		if element == "" || element == "." || element == ".." {
			return nil, fmt.Errorf(
				"the cgroup %q has the element %q, so it isn't a path under its hierarchy's root",
				path,
				element,
			)
		}
	}
	ancestry := make([]string, 0, len(elements)+1)
	for depth := len(elements); depth > 0; depth-- {
		ancestry = append(ancestry, strings.Join(elements[:depth], "/"))
	}
	return append(ancestry, "."), nil
}
