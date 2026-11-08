// Package memory_limit is the memory limit a Linux process runs under, read from its cgroups, written
// once for the programs that each read it their own way today. In cohere: command/cohere's
// memory_linux.go, which sizes its collector's ceiling. In Adamic: internal/testguard's memory.go,
// which sizes a test's child budget. In Loom: cmd/loom's buildreap.go (memoryLimitUnder, for
// build-tree and plan) and runner's machine_linux.go, which shares out a serve's capacity.
//
// The limit is the smallest one stated on the path from the process's own cgroup up to its
// hierarchy's root, since the kernel holds a cgroup to every limit above it. A process in a unit
// inside a service, with no cgroup namespace, sees the host's hierarchy: the root states none, and
// the unit's limit sits on a cgroup under the path /proc/self/cgroup names, often above the process's
// own leaf, which states none either.
//
// It is in two layers. The first is pure, over bytes: ParseMembership reads /proc/self/cgroup,
// ParseLimit a limit file and Ancestry a cgroup's path. The second is a thin reader, ReadFs, over a
// filesystem's root as an fs.FS, so a test points it at a tree it wrote. Neither holds a policy: what
// share of the limit a program takes, whether `GOMEMLIMIT` in the environment wins, and what it does
// where no limit is stated (physical memory, a default) stay each consumer's.
//
// It is a module of its own, beside git_tree, so Loom and Adamic can require it without requiring
// cohere's root module.
package memory_limit

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
)

// Source is the hierarchy a Limit was read from.
type Source string

// The answers ReadFs gives. NoLimit is an answer of its own, so no caller reads a zero Bytes as a
// cgroup that may hold nothing.
const (
	NoLimit  Source = "NoLimit"
	CgroupV2 Source = "CgroupV2"
	CgroupV1 Source = "CgroupV1"
)

// The hierarchies' mounts, as every consumer finds them today, and the files each one keeps a cgroup's
// limit and usage in. memory.max and memory.current exist only on a v2 cgroup that isn't the root.
const (
	unifiedMount   = "sys/fs/cgroup"
	memoryV1Mount  = "sys/fs/cgroup/memory"
	unifiedLimit   = "memory.max"
	unifiedUsage   = "memory.current"
	memoryV1Limit  = "memory.limit_in_bytes"
	memoryV1Usage  = "memory.usage_in_bytes"
	membershipFile = "proc/self/cgroup"
)

// ReadFs reads the limit of the process whose root system is: on Linux, ReadFs(os.DirFS("/")). It reads
// proc/self/cgroup, then walks the process's cgroup and each one above it in the hierarchy holding the
// memory controller: a v1 line naming memory where there is one (on a hybrid host, where the unified
// hierarchy has no memory controller), else the unified line. v1 is read only where the membership
// names it, never probed. A cgroup with no limit file states none and the walk goes on, so a v1
// container whose mount shows its own cgroup at the root finds its limit there. Where no cgroup states
// one, or the membership names neither hierarchy, the answer is NoLimit. A membership the kernel
// wouldn't write, a limit file that can't be read for a reason other than its absence, or one that
// isn't a limit is an error, named.
func ReadFs(system fs.FS) (Limit, error) {
	contents, readError := fs.ReadFile(system, membershipFile)
	if readError != nil {
		return Limit{}, fmt.Errorf("reading /%s: %w", membershipFile, readError)
	}
	membership, parseError := ParseMembership(contents)
	if parseError != nil {
		return Limit{}, parseError
	}
	switch {
	case membership.HasMemory:
		return walk(system, CgroupV1, memoryV1Mount, memoryV1Limit, membership.Memory)
	case membership.HasUnified:
		return walk(system, CgroupV2, unifiedMount, unifiedLimit, membership.Unified)
	}
	return Limit{Source: NoLimit}, nil
}

// walk reads limitFile in cgroup and each cgroup above it under mount, and keeps the smallest stated.
func walk(system fs.FS, source Source, mount, limitFile, cgroup string) (Limit, error) {
	ancestry, ancestryError := Ancestry(cgroup)
	if ancestryError != nil {
		return Limit{}, ancestryError
	}
	smallest := Limit{Source: NoLimit}
	for _, directory := range ancestry {
		file := path.Join(mount, directory, limitFile)
		contents, readError := fs.ReadFile(system, file)
		if errors.Is(readError, fs.ErrNotExist) {
			continue
		}
		if readError != nil {
			return Limit{}, fmt.Errorf("reading /%s: %w", file, readError)
		}
		bytes, stated, parseError := ParseLimit(contents)
		if parseError != nil {
			return Limit{}, fmt.Errorf("/%s: %w", file, parseError)
		}
		// Strictly smaller, so of two equal limits the one nearer the process wins: its usage counts
		// fewer other processes.
		if stated && (smallest.Source == NoLimit || bytes < smallest.Bytes) {
			smallest = Limit{Bytes: bytes, Source: source, Cgroup: path.Join("/", directory)}
		}
	}
	return smallest, nil
}
