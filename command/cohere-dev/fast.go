package main

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/command_line/streams"
	"github.com/system-inc/cohere/internal/testing/listed_directories"
)

// The fast tier is the edit loop (#nxgt2ca, ruled by @system_cohere on Kirk's two-tier call):
//
//	go run ./command/cohere-dev test --fast
//
// It runs every package of the module except the ones landingGateOnly names, and Go's test cache decides
// what actually runs: a package whose inputs an edit did not touch answers from the cache, and one whose
// tests import the edited code or read the edited file runs. That is the measured dependency set, kept by
// Go, so nothing here maps edits to packages.
//
// It takes no slot: it is the edit loop and has to stay instant. It prints the machine's load when it
// starts, so a fast run beside a held full gate is visible. It is never the landing gate, and says so last.

// fastTierVariable tells a package's tests that the fast tier is running them, so a test the landing gate
// alone runs can skip itself.
const fastTierVariable = "COHERE_FAST_TIER"

// fastBudgetCpuSeconds is what a package must cost in CPU, user plus system, measured alone
// with -count=1 and its test binary already built, to join landingGateOnly. Anything at or under it stays
// in the edit loop. TestEveryLandingGateOnlyEntryStillEarnsItsPlace holds every entry's recorded cost above
// it.
//
// CPU and not wall, because wall moves with the machine's load and CPU does not (#1qbez1f). With a 2s wall
// budget beside it, the registry's replay of ESLint's own rows passed two gates at 2.5s and 3.5s under load
// and failed the third at 1.1s on a quieter machine, while its CPU read 10.5 to 12.7s throughout. A gate
// whose verdict depends on who else is running is a flaky test.
//
// CPU is steadier than wall, not load-invariant. A heavily parallel package's CPU inflates under contention:
// command/cohere read 926 to 933s of CPU at load 105 to 166 against 49.4s at load about 10, and
// cross_compile 36 to 41s against 26. Every change load made was upward, so a reading at low load is the
// low one.
const fastBudgetCpuSeconds = 15

// fastEvictionCpuSeconds is what an entry, measured again now, must fall to before
// TestEveryLandingGateOnlyEntryStillEarnsItsPlace sends it back to the edit loop: a third under the budget
// to join, because a live measurement scatters and one threshold judged both ways flips on the scatter.
// One entry, a difference of two runs, read 14.9s in one of build's land runs, 18.5s at load 10 and 24.8s
// at load 13 to 50, all on 2026-10-05, and failed build's land at 14.9 against a single bar of 15. Under
// 10 is a change in what the tests do, not in who else is running.
const fastEvictionCpuSeconds = 10

// landingGateEntry is one thing the fast tier leaves to the full gate.
type landingGateEntry struct {
	// pattern is the package, as `go test` names it from the module root.
	pattern string

	// wallSeconds and cpuSeconds are its cost measured alone with -count=1 on a quiet machine, when it was
	// added.
	// TestEveryLandingGateOnlyEntryStillEarnsItsPlace measures both again and judges the CPU; the wall is
	// what the fast tier prints, for a reader, and judges nothing.
	wallSeconds, cpuSeconds float64

	reason string
}

// landingGateOnly is everything the fast tier leaves to the full gate, each measured on 2026-10-05 as its
// entry says.
var landingGateOnly = []landingGateEntry{
	{
		// At load about 10.
		pattern:     "./command/cohere",
		wallSeconds: 9.2, cpuSeconds: 49.4,
		reason: "it imports every package, so every code edit reruns it, and about 260 of its runs are the real " +
			"binary type-checking a project",
	},
	{
		// At load about 10.
		pattern:     "./internal/release/cross_compile",
		wallSeconds: 2.4, cpuSeconds: 26.0,
		reason: "it type-checks all of cohere for each of the six release targets",
	},
}

// fastTest runs the fast tier of the module at root, the working directory's when it is empty, in the environment
// given, under the bound when there is one, and returns go test's exit code. A train's pre-check runs it in the
// worktree being landed (land_train.go).
func fastTest(root string, base []string, arguments []string, limit *bound) int {
	fmt.Fprintf(streams.Errors, "cohere-dev: fast tier, at load %s\n", machineLoad())
	// Listing the module's packages takes a moment, and a person watches for the run to begin.
	streams.Show(streams.Errors)

	packages, modulePackagesError := modulePackagesIn(root, base, []string{"./..."})
	if modulePackagesError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", modulePackagesError)
		return 1
	}
	leftOut := map[string]bool{}
	for _, entry := range landingGateOnly {
		if !packages[entry.pattern] {
			fmt.Fprintf(
				streams.Errors,
				"cohere-dev: the fast tier's landing-gate list names %s, which is not a package of this "+
					"module; fix landingGateOnly in command/cohere-dev/fast.go\n",
				entry.pattern,
			)
			return 2
		}
		leftOut[entry.pattern] = true
	}

	selected := []string{}
	for pattern := range packages {
		if !leftOut[pattern] {
			selected = append(selected, pattern)
		}
	}
	sort.Strings(selected)

	code := runAttached(
		limit,
		root,
		append(slices.Clone(base), fastTierVariable+"=1"),
		"go",
		append(append([]string{"test"}, arguments...), selected...),
	)

	fmt.Fprintln(streams.Errors, "cohere-dev: the fast tier left these to the landing gate:")
	for _, entry := range landingGateOnly {
		fmt.Fprintf(streams.Errors, "  %s (%.1fs alone): %s\n", entry.pattern, entry.wallSeconds, entry.reason)
	}
	fmt.Fprintln(
		streams.Errors,
		"fast tier: not the landing gate. Run cohere-dev test ./... through the lock before landing on main.",
	)
	return code
}

// modulePackagesIn is every package patterns name in the module at root, or the working directory's when it is empty,
// named as `go test` names it from the module root: the fast tier's `./...`, the whole module, or a test's own few, so
// a test lists only the directories it judges, never every one in the tree, which go test's cache would key it on
// (#0zdyk51).
//
// The root module's only: the fast tier is not the landing gate, so it leaves the other modules go.work
// names (the shims, static_single_assignment) to the gate and to `cohere-dev test ./...` (workspace.go).
//
// It is listed in the environment the run's tests get. It once listed in the caller's own, so in a worktree with
// TypeScript a symlink, from a shell with no go flags, go list failed on VCS stamping and the fast tier
// stopped there (2026-11-05). go's stderr goes in the error, which said only "exit status 1" without it.
func modulePackagesIn(root string, environment []string, patterns []string) (map[string]bool, error) {
	command := exec.Command("go", append([]string{"list", "-f", "{{.ImportPath}} {{.Module.Path}}"}, patterns...)...)
	command.Dir = root
	command.Env = environment
	output, outputError := command.Output()
	if outputError != nil {
		if exited, ok := outputError.(*exec.ExitError); ok && len(exited.Stderr) > 0 {
			return nil, fmt.Errorf(
				"listing the module's packages: %w\n%s",
				outputError,
				strings.TrimSpace(string(exited.Stderr)),
			)
		}
		return nil, fmt.Errorf("listing the module's packages: %w", outputError)
	}
	packages := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		relative := strings.TrimPrefix(fields[0], fields[1])
		packages["."+relative] = true
	}
	// What go list walked, read again in this process, so a test that lists the module has its result keyed on a
	// package added or removed (#cmpsz8p).
	listed_directories.Open(root, patterns, nil)
	return packages, nil
}

// machineLoad is the one-minute load average, or "unknown" where it cannot be read.
func machineLoad() string {
	output, outputError := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if outputError != nil {
		if contents, readError := os.ReadFile("/proc/loadavg"); readError == nil {
			if fields := strings.Fields(string(contents)); len(fields) > 0 {
				return fields[0]
			}
		}
		return "unknown"
	}
	// "{ 3.12 2.90 2.71 }"
	if fields := strings.Fields(strings.Trim(strings.TrimSpace(string(output)), "{}")); len(fields) > 0 {
		return fields[0]
	}
	return "unknown"
}
