package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/build_inputs"
	"github.com/system-inc/cohere/internal/needs"
)

// measureLandingGateVariable opts in to measuring each landing-gate-only entry's CPU, which the nightly sets.
const measureLandingGateVariable = "COHERE_MEASURE_LANDING_GATE"

// Every entry the fast tier leaves to the landing gate was recorded above fastBudgetCPUSeconds of CPU, and
// still costs more than fastEvictionCPUSeconds measured alone now. One that got cheap belongs back in the
// edit loop, and one that names nothing is refused. An entry recorded at or under the budget is refused
// too, so one cannot be added unmeasured or below the bar; that half judges a number in this file, which no
// load moves. The live half judges CPU, user plus system time, against a bar a third lower, so the
// scatter between runs cannot flip it; the wall is logged beside it and judges nothing. An entry whose cost
// is a corpus skips naming the corpus's variable when it is unset, since its tests then skip and it would
// read as free.
//
// The measured half is opt-in, run by the nightly (bench/nightly.zsh) with COHERE_MEASURE_LANDING_GATE set:
// it judges this desk machine's CPU, so it is a measurement, and no gate runs it (#xc29pnd). The half that
// judges the numbers in this file runs everywhere, since no load moves it. Under the fast tier the whole test
// skips, since listing the module's packages is the gate's work, not the edit loop's.
//
// It measures this desk machine's budget, never a runner's: on the release's 3-core runner command/cohere alone
// took 918s, and its inner run died on Go's default ten minutes, failing on time rather than on anything it
// measures (1.0 dry run 39276248388). The opt-in keeps it to the desk, where the nightly sets it.
//
// Not parallel: the package's other tests beside it would compete for the cores and stretch what it logs.
func TestEveryLandingGateOnlyEntryStillEarnsItsPlace(test *testing.T) {
	if os.Getenv(fastTierVariable) != "" {
		test.Skip(
			"measuring the landing gate's entries is the landing gate's job, left to it by cohere-dev test --fast",
		)
	}
	root, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	packages, modulePackagesInError := modulePackagesIn(root, os.Environ())
	if modulePackagesInError != nil {
		test.Fatal(modulePackagesInError)
	}
	for _, entry := range landingGateOnly {
		// Not parallel: each entry's measurement would compete with the others' for the cores.
		test.Run(entry.pattern, func(test *testing.T) {
			if !packages[entry.pattern] {
				test.Fatalf("%s is not a package of this module", entry.pattern)
			}
			if entry.wallSeconds <= 0 || entry.cpuSeconds <= 0 || entry.reason == "" {
				test.Fatalf("%s is listed without its measured wall, CPU or reason", entry.pattern)
			}
			if entry.cpuSeconds <= fastBudgetCpuSeconds {
				test.Fatalf(
					"%s is recorded at %.1fs of CPU, at or under the fast budget of %.0fs, so it belongs in the fast tier",
					entry.pattern,
					entry.cpuSeconds,
					float64(fastBudgetCpuSeconds),
				)
			}
			needs.OptIn(test, measureLandingGateVariable, "it measures each entry's CPU on this machine, a "+
				"measurement the nightly makes, not a gate's check")
			if entry.reads.Name != "" {
				entry.reads.Root(test)
			}
			wall, cpu := measureTests(test, root, entry.pattern, nil)
			if entry.skippedBy != "" {
				fastWall, fastCpu := measureTests(test, root, entry.pattern, []string{entry.skippedBy + "=1"})
				wall, cpu = wall-fastWall, cpu-fastCpu
			}
			test.Logf(
				"%s: %.1fs wall, %.1fs CPU now (recorded %.1fs, %.1fs)",
				entry.pattern,
				wall,
				cpu,
				entry.wallSeconds,
				entry.cpuSeconds,
			)
			if cpu <= fastEvictionCpuSeconds {
				test.Errorf(
					"%s costs %.1fs of CPU alone (%.1fs of wall), at or under the eviction bar of %.0fs of CPU, so "+
						"it belongs back in the fast tier: remove it from landingGateOnly",
					entry.pattern,
					cpu,
					wall,
					float64(fastEvictionCpuSeconds),
				)
			}
		})
	}
}

// measureTests runs one package's tests with -count=1 and returns the wall and the CPU, user plus system,
// of go test and everything it waited for. The fast tier's variable is never inherited, so a measurement
// in full is in full whatever ran this test; environment adds it where a caller wants it.
//
// The test binary is built first, untimed, so the CPU is the tests' and never the compiler's: otherwise
// whichever of an entry's two measurements came first would pay for the build, and the difference between
// them would read a compile as the entry's cost.
func measureTests(test *testing.T, root string, pattern string, environment []string) (wall float64, cpu float64) {
	test.Helper()
	deadline, hasDeadline := test.Deadline()
	timeout := "-timeout=" + inheritedTimeout(deadline, hasDeadline, time.Now())
	build_inputs.Record(test, root, "-test", pattern)
	// Uncached: this run exists to do the build now, untimed, and a cached result would skip the work
	// and leave it to the measured run below.
	build := exec.Command("go", "test", "-count=1", timeout, "-run", "^$", pattern)
	build.Dir = root
	if output, outputError := build.CombinedOutput(); outputError != nil {
		test.Fatalf("building %s's tests failed, so it measured nothing: %v\n%s", pattern, outputError, output)
	}
	// Uncached: this run is the measurement, and a cached pass replays an old result in no time and no CPU.
	command := exec.Command("go", "test", "-count=1", timeout, pattern)
	command.Dir = root
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, fastTierVariable+"=") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, environment...)
	start := time.Now()
	output, outputError := command.CombinedOutput()
	if outputError != nil {
		test.Fatalf("go test -count=1 %s failed, so it measured nothing: %v\n%s", pattern, outputError, output)
	}
	return time.Since(start).Seconds(), (command.ProcessState.UserTime() + command.ProcessState.SystemTime()).Seconds()
}

// innerTimeoutMargin is how much of the outer test's time an inner run leaves, so the outer test reports
// what it measured, or that it ran out, rather than dying with it.
const innerTimeoutMargin = 30 * time.Second

// inheritedTimeout is the -timeout an inner go test gets: what the outer test has left, less the margin, or
// none when the outer test has no deadline. Otherwise the inner run dies on Go's default ten minutes, which
// the outer run's own -timeout says nothing about.
func inheritedTimeout(deadline time.Time, hasDeadline bool, now time.Time) string {
	if !hasDeadline {
		return "0"
	}
	remaining := deadline.Sub(now) - innerTimeoutMargin
	if remaining < time.Second {
		remaining = time.Second
	}
	return remaining.Round(time.Second).String()
}

func TestAnInnerRunInheritsTheOuterDeadline(test *testing.T) {
	test.Parallel()
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	for _, sample := range []struct {
		deadline    time.Time
		hasDeadline bool
		want        string
	}{
		{time.Time{}, false, "0"},
		{now.Add(30 * time.Minute), true, "29m30s"},
		{now.Add(2 * time.Hour), true, "1h59m30s"},
		{now.Add(10 * time.Second), true, "1s"},
	} {
		if got := inheritedTimeout(sample.deadline, sample.hasDeadline, now); got != sample.want {
			test.Errorf(
				"a deadline %v ahead (has one: %v) gave -timeout=%s, want %s",
				sample.deadline.Sub(now),
				sample.hasDeadline,
				got,
				sample.want,
			)
		}
	}
}

// The fast tier lists the module's packages in the environment its tests run in, not the caller's, and a
// failed listing says go's own reason. A flag go does not know fails the listing only if the environment
// given is the one go list reads.
func TestTheFastTierListsPackagesInTheRunsEnvironment(test *testing.T) {
	test.Parallel()
	root, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	environment := []string{}
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "GOFLAGS=") {
			environment = append(environment, variable)
		}
	}
	_, modulePackagesInError := modulePackagesIn(root, append(environment, "GOFLAGS=-not-a-go-flag"))
	if modulePackagesInError == nil {
		test.Fatal("go list succeeded with a go flag it does not know, so it did not run in the environment given")
	}
	if !strings.Contains(modulePackagesInError.Error(), "not-a-go-flag") {
		test.Errorf("the failed listing says %q, want go's own reason, which names the flag", modulePackagesInError)
	}
}

// The fast tier refuses -count=1 as a whole-module run does, before it runs anything: it would throw away
// the cache the fast tier rests on, across nearly every package, with no slot.
//
// Not parallel: run is the whole command, writing the package-wide streams, which only one goroutine may write; run
// beside another test that writes them, it raced on Home under -race (#bnabymg, Nov 12).
func TestTheFastTierRefusesCountOnce(test *testing.T) {
	for _, arguments := range [][]string{
		{"--fast", "-count=1"},
		{"--fast", "-count", "1"},
		{"--fast", "-v", "--count=1"},
	} {
		// --full-priority, so the refusal is all this asks of run: nothing nices this test process.
		if code := run("test", append([]string{fullPriorityFlag}, arguments...)); code != 2 {
			test.Errorf("cohere-dev test %v exited %d, want 2, the refusal", arguments, code)
		}
	}
}
