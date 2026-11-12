//go:build darwin || linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/build_inputs"
	"github.com/system-inc/cohere/internal/needs"
)

// spawnerVariable makes this test binary the planted spawner: a process that starts sleeps until it is
// refused, recording each one's pid, so the run that bounds it can be judged by what it started.
const spawnerVariable = "COHERE_DEV_BOUNDED_SPAWNER"

// spawnerCeiling is the most sleeps the spawner starts. A bound that fails lets it start them all, a number
// the machine carries, so the proof can fail without forking the machine to death.
const spawnerCeiling = 400

// A bounded run's group holds no more processes than its margin, however many it tries, and nothing it started is
// left when it ends (#fp1td9e). The planted spawner starts sleeps as fast as it can; bounded to a margin of 32, its
// group is stopped once it holds more than 32, the run says so and exits with the margin's code, and every sleep it
// started is gone with its group. The account's own count never enters it, so a busy account can't move it: on
// Home a margin taken against the account's total let all 400 sleeps start (#acdf5mc).
func TestABoundedRunStartsNoMoreThanItsMargin(test *testing.T) {
	test.Parallel()
	if pids := os.Getenv(spawnerVariable); pids != "" {
		spawn(pids)
		return
	}
	needs.Platform(test, "darwin", "on a loaded Linux box a watchdog that looks every 10ms sees the group too late "+
		"to hold it near its margin: on Home (64 cores, 3,139 threads) the spawner started 116 sleeps under 32 before "+
		"it was stopped, and only the account's limit, 16 times the margin, holds there (#fp1td9e)")
	if os.Getuid() == 0 {
		test.Skip("the kernel holds root to no process limit, so a bounded run as root is refused")
	}
	wrapper := boundedWrapper(test)
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	pids := filepath.Join(test.TempDir(), "pids")
	test.Cleanup(func() {
		// A bound that failed leaves its sleeps behind, which go when the test does.
		for _, pid := range readPids(test, pids) {
			// Ignored: a sleep the group's stop already ended is what a passing run leaves.
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	before, countError := accountProcessCount()
	if countError != nil {
		test.Fatal(countError)
	}
	command := exec.Command(wrapper, "exec", "--bounded-margin", "32", "--bounded-deadline", "2m", "--",
		executable, "-test.run=^TestABoundedRunStartsNoMoreThanItsMargin$")
	command.Env = append(outsideThePool(os.Environ()), homeVariable+"="+privatePool(test), spawnerVariable+"="+pids)
	output, runError := command.CombinedOutput()
	started := readPids(test, pids)
	test.Logf("this account had %d %s before the run; the spawner started %d sleeps\n%s",
		before, processNoun, len(started), output)
	var exited *exec.ExitError
	if !errors.As(runError, &exited) || exited.ExitCode() != marginExitCode ||
		!strings.Contains(string(output), "past the bounded run's margin of 32") {
		test.Errorf("the bounded spawner ended with %v, want exit %d with the margin named:\n%s",
			runError, marginExitCode, output)
	}
	if len(started) >= spawnerCeiling {
		test.Fatalf("the spawner started all %d sleeps under a margin of 32, so the run was not bounded",
			spawnerCeiling)
	}
	// The watchdog looks every 10ms, and the spawner starts a sleep about every millisecond, so it may pass the
	// margin by what it starts between two looks.
	if len(started) > 32+64 {
		test.Errorf("the spawner started %d sleeps under a margin of 32", len(started))
	}
	for _, pid := range started {
		if !gone(pid) {
			test.Errorf("sleep %d the spawner started is still running after the bounded run ended", pid)
		}
	}
	after, countError := accountProcessCount()
	if countError != nil {
		test.Fatal(countError)
	}
	test.Logf("this account has %d %s after the run", after, processNoun)
}

// spawn is the planted spawner: it starts sleeps, writing each pid to the file as it goes, until a start is
// refused or it reaches the ceiling, and exits leaving them running. Refused, it exits 3; never refused, it
// stops what it started and exits 0, which a bounded run must never see.
func spawn(pids string) {
	say := func(format string, values ...any) {
		// Ignored: the spawner's words only reach the test's log, and its exit code and the pid file carry
		// the verdict, so a word that did not arrive changes nothing.
		_, _ = fmt.Fprintf(os.Stderr, format+"\n", values...)
	}
	file, createError := os.Create(pids)
	if createError != nil {
		say("%v", createError)
		os.Exit(1)
	}
	started := []*exec.Cmd{}
	for len(started) < spawnerCeiling {
		sleep := exec.Command("sleep", "60")
		if startError := sleep.Start(); startError != nil {
			say("refused after %d: %v", len(started), startError)
			os.Exit(3)
		}
		started = append(started, sleep)
		if _, writeError := fmt.Fprintln(file, sleep.Process.Pid); writeError != nil {
			say("%v", writeError)
			os.Exit(1)
		}
	}
	for _, sleep := range started {
		// Ignored: a sleep that already ended needs no stop.
		_ = sleep.Process.Kill()
	}
	say("never refused in %d starts", spawnerCeiling)
	os.Exit(0)
}

// A bounded run that passes its deadline is stopped with its whole group, a child it left running beside it
// included, and exits with the deadline's code, saying so.
func TestABoundedRunStopsItsGroupAtTheDeadline(test *testing.T) {
	test.Parallel()
	if os.Getuid() == 0 {
		test.Skip("the kernel holds root to no process limit, so a bounded run as root is refused")
	}
	wrapper := boundedWrapper(test)
	pid := filepath.Join(test.TempDir(), "pid")
	command := exec.Command(wrapper, "exec", "--bounded-deadline", "1s", "--",
		"sh", "-c", "sleep 60 & echo $! > '"+pid+"'; sleep 60")
	command.Env = append(outsideThePool(os.Environ()), homeVariable+"="+privatePool(test))
	began := time.Now()
	output, runError := command.CombinedOutput()
	elapsed := time.Since(began)
	var exited *exec.ExitError
	if !errors.As(runError, &exited) || exited.ExitCode() != deadlineExitCode {
		test.Errorf("the run ended with %v, want exit code %d:\n%s", runError, deadlineExitCode, output)
	}
	if !strings.Contains(string(output), "passed the bounded run's deadline of 1s") {
		test.Errorf("the run did not say its deadline stopped it:\n%s", output)
	}
	// Timing assertion: a deadline of 1s against 30s survives any load the pool allows, and a run its deadline
	// failed to stop runs its command's 60s.
	if elapsed > 30*time.Second {
		test.Errorf("a run with a 1s deadline took %s to end", elapsed)
	}
	for _, child := range readPids(test, pid) {
		if !gone(child) {
			test.Errorf("the sleep the command left beside it, %d, outlived the deadline", child)
		}
	}
}

// An interrupt to cohere-dev reaches the bounded run's group, which no longer hears the terminal, and what the
// command leaves behind is stopped with it: the shell's background sleep ignores the interrupt, as a
// background job does, and is stopped as a leftover.
func TestAnInterruptReachesABoundedRunsGroup(test *testing.T) {
	test.Parallel()
	if os.Getuid() == 0 {
		test.Skip("the kernel holds root to no process limit, so a bounded run as root is refused")
	}
	wrapper := boundedWrapper(test)
	pid := filepath.Join(test.TempDir(), "pid")
	command := exec.Command(wrapper, "exec", "--bounded", "--",
		"sh", "-c", "sleep 60 & echo $! > '"+pid+"'; sleep 60")
	command.Env = append(outsideThePool(os.Environ()), homeVariable+"="+privatePool(test))
	if startError := command.Start(); startError != nil {
		test.Fatal(startError)
	}
	for deadline := time.Now().Add(time.Minute); len(readPids(test, pid)) == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			// Ignored: the run is stopped because the test is failing; its error says nothing more.
			_ = command.Process.Kill()
			test.Fatal("waited a minute for the bounded command to start its sleep")
		}
	}
	if signalError := command.Process.Signal(os.Interrupt); signalError != nil {
		test.Fatal(signalError)
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		// Ignored: the run is stopped because the test is failing; its error says nothing more.
		_ = command.Process.Kill()
		test.Fatal("the bounded run was still running 30s after an interrupt")
	}
	for _, child := range readPids(test, pid) {
		if !gone(child) {
			test.Errorf("the background sleep %d outlived the interrupted run", child)
		}
	}
}

// boundedWrapper builds cohere-dev for a test of a bounded run.
func boundedWrapper(test *testing.T) string {
	test.Helper()
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	return wrapper
}

// readPids reads the pids a file lists one to a line, none when it is missing.
func readPids(test *testing.T, path string) []int {
	test.Helper()
	pids := []int{}
	for _, line := range strings.Fields(string(readMaybeMissing(test, path))) {
		pid, parseError := strconv.Atoi(line)
		if parseError != nil {
			test.Fatalf("%s holds %q, not a pid", path, line)
		}
		pids = append(pids, pid)
	}
	return pids
}

// gone reports whether a process has ended, waiting a few seconds for the system to reap it: one stopped as
// the run ended may still be a zombie until its new parent collects it.
func gone(pid int) bool {
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
	}
}
