package memory_limit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/memory_limit"
)

// writeRoot writes files, each a path from a filesystem's root to its contents, into a directory of the
// test's own and returns it, so ReadFs reads it through os.DirFS as it reads "/" on a host.
func writeRoot(test *testing.T, files map[string]string) string {
	test.Helper()
	root := test.TempDir()
	for name, contents := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(file), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(file, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return root
}

const sixteenGibibytes = "17179869184\n"

// Each tree a host shows, and the limit its process runs under. A cgroup directory with no limit file
// is written as some other file beside where its limit would be, so the directory is there and states
// nothing.
func TestReadFsKeepsTheSmallestLimitOnThePathToTheRoot(test *testing.T) {
	test.Parallel()
	for _, check := range []struct {
		name  string
		files map[string]string
		want  memory_limit.Limit
		usage string
	}{
		{
			// Loom's unit: the service states "max", the unit 16 GiB, and the command's leaf under it
			// none. The usage the limit leaves is the unit's, not the leaf's.
			name: "v2 nested in a unit",
			files: map[string]string{
				"proc/self/cgroup":                                                        "0::/system.slice/loom-serve.service/unit-a/cmd-1\n",
				"sys/fs/cgroup/cgroup.controllers":                                        "cpu memory\n",
				"sys/fs/cgroup/system.slice/loom-serve.service/memory.max":                "max\n",
				"sys/fs/cgroup/system.slice/loom-serve.service/unit-a/memory.max":         sixteenGibibytes,
				"sys/fs/cgroup/system.slice/loom-serve.service/unit-a/cmd-1/cgroup.procs": "1\n",
			},
			want: memory_limit.Limit{
				Bytes:  16 << 30,
				Source: memory_limit.CgroupV2,
				Cgroup: "/system.slice/loom-serve.service/unit-a",
			},
			usage: "sys/fs/cgroup/system.slice/loom-serve.service/unit-a/memory.current",
		},
		{
			// Adamic's case: the leaf states none, its parent a larger limit, and the smaller one two
			// levels up is the one the kernel holds the process to.
			name: "the smaller limit two levels up",
			files: map[string]string{
				"proc/self/cgroup":                             "0::/capped/inner/leaf\n",
				"sys/fs/cgroup/capped/memory.max":              sixteenGibibytes,
				"sys/fs/cgroup/capped/inner/memory.max":        "34359738368\n",
				"sys/fs/cgroup/capped/inner/leaf/cgroup.procs": "1\n",
			},
			want:  memory_limit.Limit{Bytes: 16 << 30, Source: memory_limit.CgroupV2, Cgroup: "/capped"},
			usage: "sys/fs/cgroup/capped/memory.current",
		},
		{
			name: "a smaller limit above the process's own",
			files: map[string]string{
				"proc/self/cgroup":                             "0::/hot.slice/own.scope\n",
				"sys/fs/cgroup/hot.slice/memory.max":           "90194313216\n",
				"sys/fs/cgroup/hot.slice/own.scope/memory.max": "107374182400\n",
			},
			want: memory_limit.Limit{Bytes: 90194313216, Source: memory_limit.CgroupV2, Cgroup: "/hot.slice"},
		},
		{
			name: "the process's own limit",
			files: map[string]string{
				"proc/self/cgroup":                                "0::/build.slice/tree.scope\n",
				"sys/fs/cgroup/build.slice/memory.max":            "max\n",
				"sys/fs/cgroup/build.slice/tree.scope/memory.max": sixteenGibibytes,
			},
			want: memory_limit.Limit{
				Bytes:  16 << 30,
				Source: memory_limit.CgroupV2,
				Cgroup: "/build.slice/tree.scope",
			},
		},
		{
			// A container with a cgroup namespace of its own: its root is the container's cgroup.
			name: "v2 at a namespace's root",
			files: map[string]string{
				"proc/self/cgroup":         "0::/\n",
				"sys/fs/cgroup/memory.max": "8589934592\n",
			},
			want:  memory_limit.Limit{Bytes: 8 << 30, Source: memory_limit.CgroupV2, Cgroup: "/"},
			usage: "sys/fs/cgroup/memory.current",
		},
		{
			name: "v2 at the host's root",
			files: map[string]string{
				"proc/self/cgroup":                 "0::/\n",
				"sys/fs/cgroup/cgroup.controllers": "cpu memory\n",
			},
			want: memory_limit.Limit{Source: memory_limit.NoLimit},
		},
		{
			name: "every limit max",
			files: map[string]string{
				"proc/self/cgroup":                 "0::/a/b\n",
				"sys/fs/cgroup/a/memory.max":       "max\n",
				"sys/fs/cgroup/a/b/memory.max":     "max\n",
				"sys/fs/cgroup/cgroup.controllers": "memory\n",
			},
			want: memory_limit.Limit{Source: memory_limit.NoLimit},
		},
		{
			name:  "a cgroup the mount doesn't show",
			files: map[string]string{"proc/self/cgroup": "0::/nowhere\n"},
			want:  memory_limit.Limit{Source: memory_limit.NoLimit},
		},
		{
			name: "v1",
			files: map[string]string{
				"proc/self/cgroup":                                      "12:pids:/docker/abc\n5:memory:/docker/abc\n1:name=systemd:/docker/abc\n",
				"sys/fs/cgroup/memory/memory.limit_in_bytes":            "9223372036854771712\n",
				"sys/fs/cgroup/memory/docker/memory.limit_in_bytes":     "9223372036854771712\n",
				"sys/fs/cgroup/memory/docker/abc/memory.limit_in_bytes": "2147483648\n",
			},
			want:  memory_limit.Limit{Bytes: 2 << 30, Source: memory_limit.CgroupV1, Cgroup: "/docker/abc"},
			usage: "sys/fs/cgroup/memory/docker/abc/memory.usage_in_bytes",
		},
		{
			// Without a cgroup namespace a v1 container's mount shows its own cgroup at the root, so
			// the path its membership names isn't there and the walk finds the limit at the mount.
			name: "v1 in a container",
			files: map[string]string{
				"proc/self/cgroup":                           "5:memory:/docker/abc\n",
				"sys/fs/cgroup/memory/memory.limit_in_bytes": "2147483648\n",
			},
			want: memory_limit.Limit{Bytes: 2 << 30, Source: memory_limit.CgroupV1, Cgroup: "/"},
		},
		{
			name: "v1 unlimited with 64 KiB pages",
			files: map[string]string{
				"proc/self/cgroup": "4:memory:/user.slice\n",
				"sys/fs/cgroup/memory/user.slice/memory.limit_in_bytes": "9223372036854710272\n",
			},
			want: memory_limit.Limit{Source: memory_limit.NoLimit},
		},
		{
			// On a hybrid host the unified hierarchy holds no memory controller, so v1's line wins.
			name: "hybrid",
			files: map[string]string{
				"proc/self/cgroup":                                      "0::/user.slice\n5:memory:/user.slice\n",
				"sys/fs/cgroup/user.slice/memory.max":                   "1073741824\n",
				"sys/fs/cgroup/memory/user.slice/memory.limit_in_bytes": "4294967296\n",
			},
			want: memory_limit.Limit{Bytes: 4 << 30, Source: memory_limit.CgroupV1, Cgroup: "/user.slice"},
		},
		{
			name:  "no hierarchy named",
			files: map[string]string{"proc/self/cgroup": "1:name=systemd:/user.slice\n"},
			want:  memory_limit.Limit{Source: memory_limit.NoLimit},
		},
	} {
		got, readError := memory_limit.ReadFs(os.DirFS(writeRoot(test, check.files)))
		if readError != nil || got != check.want {
			test.Errorf("%s: ReadFs %+v (%v), want %+v", check.name, got, readError, check.want)
			continue
		}
		if check.usage != "" && got.UsageFile() != check.usage {
			test.Errorf("%s: UsageFile %q, want %q", check.name, got.UsageFile(), check.usage)
		}
	}
}

// What ReadFs can't read is an error naming what, never a limit that isn't there.
func TestReadFsRefusesWhatItCantRead(test *testing.T) {
	test.Parallel()
	for _, check := range []struct {
		name, mentions string
		files          map[string]string
	}{
		{"no membership", "/proc/self/cgroup", map[string]string{"proc/meminfo": "MemTotal: 1 kB\n"}},
		{
			"a limit that isn't one",
			"/sys/fs/cgroup/a/memory.max",
			map[string]string{"proc/self/cgroup": "0::/a\n", "sys/fs/cgroup/a/memory.max": "lots\n"},
		},
		{
			"a cgroup outside the namespace",
			"..",
			map[string]string{"proc/self/cgroup": "0::/../sibling\n", "sys/fs/cgroup/memory.max": "1\n"},
		},
		{"a membership the kernel wouldn't write", "isn't", map[string]string{"proc/self/cgroup": "0:/a\n"}},
	} {
		got, readError := memory_limit.ReadFs(os.DirFS(writeRoot(test, check.files)))
		if readError == nil || !strings.Contains(readError.Error(), check.mentions) {
			test.Errorf("%s: ReadFs %+v (%v), want an error naming %q", check.name, got, readError, check.mentions)
		}
	}
}

// A NoLimit answer names no usage file, so no caller reads the root's, which v2 doesn't keep.
func TestNoLimitHasNoUsageFile(test *testing.T) {
	test.Parallel()
	if file := (memory_limit.Limit{Source: memory_limit.NoLimit}).UsageFile(); file != "" {
		test.Errorf("UsageFile %q", file)
	}
}

func TestParseLimit(test *testing.T) {
	test.Parallel()
	for _, check := range []struct {
		contents string
		bytes    uint64
		stated   bool
	}{
		{"17179869184", 16 << 30, true},
		{"17179869184\n", 16 << 30, true},
		{" 17179869184 \n", 16 << 30, true},
		{"0\n", 0, true},
		{"max\n", 0, false},
		{"max", 0, false},
		{"", 0, false},
		{"\n", 0, false},
		{"9223372036854771712\n", 0, false},
		{"9223372036854710272\n", 0, false},
		{"1152921504606846975\n", 1<<60 - 1, true},
	} {
		bytes, stated, parseError := memory_limit.ParseLimit([]byte(check.contents))
		if parseError != nil || bytes != check.bytes || stated != check.stated {
			test.Errorf(
				"%q: %d %t (%v), want %d %t",
				check.contents,
				bytes,
				stated,
				parseError,
				check.bytes,
				check.stated,
			)
		}
	}
	for _, contents := range []string{"lots", "-1", "16G", "18446744073709551616", "1 2"} {
		if bytes, stated, parseError := memory_limit.ParseLimit([]byte(contents)); parseError == nil {
			test.Errorf("%q read as %d %t", contents, bytes, stated)
		}
	}
}

func TestParseMembership(test *testing.T) {
	test.Parallel()
	for _, check := range []struct {
		contents string
		want     memory_limit.Membership
	}{
		{"0::/a/b\n", memory_limit.Membership{Unified: "/a/b", HasUnified: true}},
		{"0::/\n", memory_limit.Membership{Unified: "/", HasUnified: true}},
		{"0::/a:b\n", memory_limit.Membership{Unified: "/a:b", HasUnified: true}},
		{
			"11:cpu,cpuacct:/x\n4:memory:/docker/abc\n1:name=systemd:/x\n",
			memory_limit.Membership{Memory: "/docker/abc", HasMemory: true},
		},
		{"3:blkio,memory:/y\n", memory_limit.Membership{Memory: "/y", HasMemory: true}},
		{
			"5:memory:/u\n0::/u\n",
			memory_limit.Membership{Unified: "/u", HasUnified: true, Memory: "/u", HasMemory: true},
		},
		{"", memory_limit.Membership{}},
	} {
		got, parseError := memory_limit.ParseMembership([]byte(check.contents))
		if parseError != nil || got != check.want {
			test.Errorf("%q: %+v (%v), want %+v", check.contents, got, parseError, check.want)
		}
	}
	for _, contents := range []string{"0:/a\n", "x::/a\n", "0::/a\n0::/b\n", "4:memory:/a\n5:memory:/b\n"} {
		if got, parseError := memory_limit.ParseMembership([]byte(contents)); parseError == nil {
			test.Errorf("%q read as %+v", contents, got)
		}
	}
}

func TestAncestry(test *testing.T) {
	test.Parallel()
	for path, want := range map[string]string{
		"/":                "[.]",
		"/a":               "[a .]",
		"/a/b":             "[a/b a .]",
		"/s.slice/u/cmd-1": "[s.slice/u/cmd-1 s.slice/u s.slice .]",
	} {
		ancestry, ancestryError := memory_limit.Ancestry(path)
		if got := "[" + strings.Join(ancestry, " ") + "]"; ancestryError != nil || got != want {
			test.Errorf("%q: %s (%v), want %s", path, got, ancestryError, want)
		}
	}
	for _, path := range []string{"", "a/b", "/..", "/../a", "/a/./b", "/a//b", "/a/"} {
		if ancestry, ancestryError := memory_limit.Ancestry(path); ancestryError == nil {
			test.Errorf("%q read as %v", path, ancestry)
		}
	}
}
