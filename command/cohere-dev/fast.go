package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// The fast tier is the edit loop (#nxgt2ca, ruled by @system_cohere on Kirk's two-tier call):
//
//	go run ./command/cohere-dev test --fast
//
// It runs every package of the module except the ones landingGateOnly names, and Go's test cache decides
// what actually runs: a package whose inputs an edit did not touch answers from the cache, and one whose
// tests import the edited code or read the edited file runs. That is the measured dependency set, kept by
// Go, so nothing here maps edits to packages. The corpus walks are left out inside their package by the
// variable fastTierVariable names, which their helpers read.
//
// It takes no slot: it is the edit loop and has to stay instant. It prints the machine's load when it
// starts, so a fast run beside a held full gate is visible. It is never the landing gate, and says so last.

// fastTierVariable tells a package's tests that the fast tier is running them, so a test the landing gate
// alone runs can skip itself. The corpus walks in high_level_intermediate_representation read it.
const fastTierVariable = "COHERE_FAST_TIER"

// fastBudgetCPUSeconds is what an entry in landingGateOnly must still cost in CPU, user plus system,
// measured alone with -count=1 and its test binary already built, to stay out of the fast tier. An entry at
// or under it belongs back in the edit loop, and TestEveryLandingGateOnlyEntryStillEarnsItsPlace fails until
// it is moved.
//
// CPU and not wall, because wall moves with the machine's load and CPU does not (#1qbez1f). With a 2s wall
// budget beside it, the registry's ESLint corpus passed two gates at 2.5s and 3.5s under load and failed the
// third at 1.1s on a quieter machine, while its CPU read 10.5 to 12.7s throughout. A gate whose verdict
// depends on who else is running is a flaky test.
//
// CPU is steadier than wall, not load-invariant. A heavily parallel package's CPU inflates under contention:
// command/cohere read 926 to 933s of CPU at load 105 to 166 against 49.4s at load about 10, and
// crosscompile 36 to 41s against 26, while hir held at 29 to 31s at every load measured. Every change load
// made tonight was upward, so a verdict at low load is the strict one, and every entry today clears 15 at
// load about 10 and at load 166 alike.
const fastBudgetCPUSeconds = 15

// landingGateEntry is one thing the fast tier leaves to the full gate.
type landingGateEntry struct {
	// pattern is the package, as `go test` names it from the module root.
	pattern string

	// skippedBy is the variable its tests read to leave themselves out, for an entry that is part of a
	// package rather than all of it; empty for a whole package.
	skippedBy string

	// wallSeconds and cpuSeconds are its cost measured alone with -count=1 on a quiet machine, when it was
	// added; for part of a package, the package run in full less the package run in the fast tier.
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
		pattern:     "./internal/release/crosscompile",
		wallSeconds: 2.4, cpuSeconds: 26.0,
		reason: "it type-checks all of cohere for each of the six release targets",
	},
	{
		pattern:   "./internal/lint/ecmascript/high_level_intermediate_representation",
		skippedBy: fastTierVariable,
		// At load 80 to 100, so the wall is high: the package in full took 13.2s and 33.5s of CPU, in the fast
		// tier 2.4s and 4.5s.
		wallSeconds: 10.8, cpuSeconds: 29.0,
		reason: "the corpus walks lower hundreds of real files, again in each of about 45 tests; the package's " +
			"other tests stay in the fast tier",
	},
}

// fastTest runs the fast tier and returns go test's exit code.
func fastTest(arguments []string) int {
	// The fast tier rests on Go's test cache, and runs nearly every package with no slot, so -count=1 here
	// is the costliest run there is on a loaded machine (@system_cohere_build's catch).
	if countsOnce(arguments) {
		refuseCountOnce()
		return 2
	}
	fmt.Fprintf(os.Stderr, "cohere-dev: fast tier, at load %s\n", machineLoad())

	packages, err := modulePackages()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
		return 1
	}
	skippedPackages := map[string]bool{}
	for _, entry := range landingGateOnly {
		if !packages[entry.pattern] {
			fmt.Fprintf(os.Stderr, "cohere-dev: the fast tier's landing-gate list names %s, which is not a package of this "+
				"module; fix landingGateOnly in command/cohere-dev/fast.go\n", entry.pattern)
			return 2
		}
		if entry.skippedBy == "" {
			skippedPackages[entry.pattern] = true
		}
	}

	selected := []string{}
	for pattern := range packages {
		if !skippedPackages[pattern] {
			selected = append(selected, pattern)
		}
	}
	sort.Strings(selected)

	command := exec.Command("go", append(append([]string{"test"}, arguments...), selected...)...)
	command.Env = append(os.Environ(), fastTierVariable+"=1")
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	code := 0
	if err := command.Run(); err != nil {
		exited, ok := err.(*exec.ExitError)
		if !ok {
			fmt.Fprintf(os.Stderr, "cohere-dev: running go test: %v\n", err)
			return 1
		}
		code = exited.ExitCode()
	}

	fmt.Fprintln(os.Stderr, "cohere-dev: the fast tier left these to the landing gate:")
	for _, entry := range landingGateOnly {
		what := entry.pattern
		if entry.skippedBy != "" {
			what += "'s corpus walks"
		}
		fmt.Fprintf(os.Stderr, "  %s (%.1fs alone): %s\n", what, entry.wallSeconds, entry.reason)
	}
	fmt.Fprintln(os.Stderr, "fast tier: not the landing gate. Run cohere-dev test ./... through the lock before landing on main.")
	return code
}

// modulePackages is every package of the module, named as `go test` names it from the module root.
func modulePackages() (map[string]bool, error) {
	return modulePackagesIn("")
}

// modulePackagesIn is modulePackages for the module at root, or the working directory's when it is empty.
func modulePackagesIn(root string) (map[string]bool, error) {
	command := exec.Command("go", "list", "-f", "{{.ImportPath}} {{.Module.Path}}", "./...")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("listing the module's packages: %w", err)
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
	return packages, nil
}

// machineLoad is the one-minute load average, or "unknown" where it cannot be read.
func machineLoad() string {
	output, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
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
