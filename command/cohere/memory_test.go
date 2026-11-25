package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryPolicyTurnsCollectionOffUnderHalfOfWhatIsAvailable(test *testing.T) {
	test.Parallel()
	noEnvironment := func(string) (string, bool) { return "", false }
	policy := decideMemoryPolicy(noEnvironment, func() (uint64, error) { return 108e9, nil })
	if policy.ceiling != 54e9 {
		test.Fatalf("ceiling %d, want half of 108 GB", policy.ceiling)
	}
	if got, want := policy.line(), "memory: collector off, ceiling 54 GB (half of 108 GB available)"; got != want {
		test.Fatalf("line %q, want %q", got, want)
	}
	small := decideMemoryPolicy(noEnvironment, func() (uint64, error) { return 7e9, nil })
	if got, want := small.line(), "memory: collector off, ceiling 3.5 GB (half of 7.0 GB available)"; got != want {
		test.Fatalf("line %q, want %q", got, want)
	}
}

func TestMemoryPolicyStepsAsideForTheEnvironment(test *testing.T) {
	test.Parallel()
	for _, set := range []map[string]string{
		{"GOGC": "100"},
		{"GOMEMLIMIT": "2GiB"},
		{"GOGC": "off", "GOMEMLIMIT": "6GiB"},
	} {
		lookup := func(name string) (string, bool) { value, ok := set[name]; return value, ok }
		asked := false
		policy := decideMemoryPolicy(lookup, func() (uint64, error) { asked = true; return 108e9, nil })
		if policy.ceiling != 0 || asked {
			test.Fatalf(
				"%v: the policy set a ceiling of %d (memory read: %v) where the environment decides",
				set,
				policy.ceiling,
				asked,
			)
		}
		for name, value := range set {
			if !strings.Contains(policy.line(), name+"="+value) {
				test.Fatalf("%v: line %q does not name %s", set, policy.line(), name)
			}
		}
	}
}

func TestMemoryPolicyKeepsTheDefaultWhenMemoryCannotBeRead(test *testing.T) {
	test.Parallel()
	noEnvironment := func(string) (string, bool) { return "", false }
	for _, reading := range []func() (uint64, error){
		func() (uint64, error) { return 0, errors.New("no reader") },
		func() (uint64, error) { return 0, nil },
	} {
		policy := decideMemoryPolicy(noEnvironment, reading)
		if policy.ceiling != 0 || !strings.Contains(policy.line(), "Go's default") {
			test.Fatalf("an unreadable reading turned collection off: %+v, %q", policy, policy.line())
		}
	}
}

func TestDarwinAvailableMemoryCountsFreeFileBackedAndPurgeablePages(test *testing.T) {
	test.Parallel()
	if got := darwinAvailableMemory(16384, 2_898_869, 1_800_168, 57_667); got != 16384*(2_898_869+1_800_168+57_667) {
		test.Fatalf("got %d", got)
	}
}

func TestLinuxAvailableMemoryTakesTheSmallerOfMemAvailableAndTheCgroup(test *testing.T) {
	test.Parallel()
	meminfo := []byte("MemTotal:       16384000 kB\nMemFree:         1000000 kB\nMemAvailable:    8000000 kB\n")
	host := uint64(8_000_000 * 1024)
	cases := []struct {
		name   string
		cgroup linuxCgroupMemory
		want   uint64
	}{
		{"no cgroup", linuxCgroupMemory{}, host},
		{"v2 without a limit", linuxCgroupMemory{v2Maximum: []byte("max\n"), v2Current: []byte("1000\n")}, host},
		{
			"v2 limit with usage",
			linuxCgroupMemory{v2Maximum: []byte("4294967296\n"), v2Current: []byte("1073741824\n")},
			3 << 30,
		},
		{"v2 limit without usage", linuxCgroupMemory{v2Maximum: []byte("4294967296\n")}, 4 << 30},
		{"v2 over its limit", linuxCgroupMemory{v2Maximum: []byte("1000\n"), v2Current: []byte("2000\n")}, 0},
		{"v1 unlimited", linuxCgroupMemory{v1Limit: []byte("9223372036854771712\n"), v1Usage: []byte("1000\n")}, host},
		{
			"v1 limit with usage",
			linuxCgroupMemory{v1Limit: []byte("2147483648\n"), v1Usage: []byte("1073741824\n")},
			1 << 30,
		},
		{
			"a cgroup above the host",
			linuxCgroupMemory{v2Maximum: []byte("68719476736\n"), v2Current: []byte("0\n")},
			host,
		},
	}
	for _, entry := range cases {
		got, linuxAvailableMemoryError := linuxAvailableMemory(meminfo, entry.cgroup)
		if linuxAvailableMemoryError != nil || got != entry.want {
			test.Errorf("%s: got %d, %v; want %d", entry.name, got, linuxAvailableMemoryError, entry.want)
		}
	}
	if _, linuxAvailableMemoryError := linuxAvailableMemory(
		[]byte("MemTotal: 1 kB\n"),
		linuxCgroupMemory{},
	); linuxAvailableMemoryError == nil {
		test.Error("a meminfo without MemAvailable read as a number")
	}
}

// A Loom unit, with no cgroup namespace: the root states no limit, the unit's cgroup two levels down
// states 16 GiB and holds 4 GiB, and the command's leaf under it states none. The room is the unit's 12
// GiB, not the host's 60 GiB available, so a run in a unit collects before the kernel kills it (#mf4x0hb).
func TestLinuxAvailableMemoryReadsTheLimitOfTheUnitAboveItsLeaf(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	for name, contents := range map[string]string{
		"proc/meminfo":                                                 "MemAvailable: 62914560 kB\n",
		"proc/self/cgroup":                                             "0::/loom-serve.service/unit-a/cmd-1\n",
		"sys/fs/cgroup/cgroup.controllers":                             "cpu memory\n",
		"sys/fs/cgroup/loom-serve.service/memory.max":                  "max\n",
		"sys/fs/cgroup/loom-serve.service/memory.current":              "8589934592\n",
		"sys/fs/cgroup/loom-serve.service/unit-a/memory.max":           "17179869184\n",
		"sys/fs/cgroup/loom-serve.service/unit-a/memory.current":       "4294967296\n",
		"sys/fs/cgroup/loom-serve.service/unit-a/cmd-1/memory.current": "1073741824\n",
	} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(file), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(file, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	got, linuxAvailableMemoryError := linuxAvailableMemoryFrom(os.DirFS(root))
	if want := uint64(12 << 30); linuxAvailableMemoryError != nil || got != want {
		test.Fatalf("got %d (%v), want the unit's room, %d", got, linuxAvailableMemoryError, want)
	}
}

func TestATestBinaryKeepsTheDefaultCollector(test *testing.T) {
	test.Parallel()
	if !activeMemoryPolicy.testBinary || activeMemoryPolicy.ceiling != 0 {
		test.Fatalf("the test binary's policy is %+v, which would hold every graph it builds", activeMemoryPolicy)
	}
}

// One cohere run checking several projects never piles into itself: the project children it runs at once share its
// half of what is available, each through the environment runProject starts it with, so two at once take a quarter
// each and a child alone takes the half (#jrr6a5x). A share inherited from a parent run is replaced, never divided
// again.
func TestProjectChildrenAtOnceShareTheRunsHalf(test *testing.T) {
	test.Parallel()
	const available = 64e9
	for _, each := range []struct {
		name        string
		launches    int
		runsAtOnce  int
		inherited   []string
		wantCeiling uint64
		wantLine    string
	}{
		{name: "two children at once take a quarter each", launches: 2, runsAtOnce: 4, wantCeiling: 16e9,
			wantLine: "memory: collector off, ceiling 16 GB (half of 64 GB available, shared by 2 projects at once)"},
		{name: "six projects four at once take an eighth each", launches: 6, runsAtOnce: 4, wantCeiling: 8e9,
			wantLine: "memory: collector off, ceiling 8.0 GB (half of 64 GB available, shared by 4 projects at once)"},
		{name: "a child alone takes the half", launches: 1, runsAtOnce: 4, wantCeiling: 32e9,
			wantLine: "memory: collector off, ceiling 32 GB (half of 64 GB available)"},
		{name: "a child alone drops a share it inherited", launches: 1, runsAtOnce: 4,
			inherited: []string{memoryShareVariable + "=3"}, wantCeiling: 32e9,
			wantLine: "memory: collector off, ceiling 32 GB (half of 64 GB available)"},
		{name: "two children replace a share they inherited", launches: 2, runsAtOnce: 4,
			inherited: []string{memoryShareVariable + "=3"}, wantCeiling: 16e9,
			wantLine: "memory: collector off, ceiling 16 GB (half of 64 GB available, shared by 2 projects at once)"},
	} {
		environment := projectChildEnvironment(append([]string{"HOME=/nowhere"}, each.inherited...),
			discoveredProject{Kind: projectKindGoModule}, "go", "", "", "", projectMemoryShare(each.launches,
				each.runsAtOnce))
		lookupEnvironment := func(name string) (string, bool) {
			for _, entry := range environment {
				if value, found := strings.CutPrefix(entry, name+"="); found {
					return value, true
				}
			}
			return "", false
		}
		policy := decideMemoryPolicy(lookupEnvironment, func() (uint64, error) { return available, nil })
		if policy.ceiling != each.wantCeiling {
			test.Errorf("%s: the child's ceiling is %d, want %d (environment %v)", each.name, policy.ceiling,
				each.wantCeiling, environment)
		}
		if got := policy.line(); got != each.wantLine {
			test.Errorf("%s: line %q, want %q", each.name, got, each.wantLine)
		}
	}
}

// A share only cohere's parent writes is read as no share when it is not a whole number of at least 1.
func TestAMalformedMemoryShareIsNoShare(test *testing.T) {
	test.Parallel()
	for _, value := range []string{"0", "-2", "two", ""} {
		lookupEnvironment := func(name string) (string, bool) {
			if name == memoryShareVariable {
				return value, true
			}
			return "", false
		}
		if share := memoryShare(lookupEnvironment); share != 1 {
			test.Errorf("%s=%q is a share of %d, want 1", memoryShareVariable, value, share)
		}
	}
}
