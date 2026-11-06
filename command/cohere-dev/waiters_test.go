package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A waiter names each new holder, not only the first it saw (#vz3g7cn). On a one-token machine the test
// holds the token, two runs queue behind it in a known order, and the test lets go: the first waiter takes
// the token, and the second, still waiting, names that run as the holder now. Before, it went on naming the
// holder it first saw, after that holder was gone.
//
// The second run holds the token until the test has read the third naming it, not for a fixed time: a waiter
// looks once a second, and a two-second hold at load 20 to 30 let a look fall outside it, so the third took
// the token without ever having seen the second hold it (swift's land, 2026-11-06 12:53). A waiter that
// never names the second is still caught, at the deadline.
func TestAWaiterNamesEachNewHolder(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("tokens are a Unix lock")
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	home := privatePool(test)
	slots := filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots")
	environment := append(outsideThePool(os.Environ()), homeVariable+"="+home, tokensVariable+"=1")

	first, takeSlotError := takeSlot(slots, 1, "the first holder, which the test lets go")
	if takeSlotError != nil || first == nil {
		test.Fatalf("taking the token: %v", takeSlotError)
	}
	tickets := func() int {
		entries := listMaybeMissing(test, queueDirectory(slots))
		live := 0
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".") {
				live++
			}
		}
		return live
	}
	release := filepath.Join(test.TempDir(), "release")
	type waiter struct {
		command *exec.Cmd
		output  string
	}
	queue := func(marker string, want int) waiter {
		output := filepath.Join(test.TempDir(), "output")
		file, createError := os.Create(output)
		if createError != nil {
			test.Fatal(createError)
		}
		defer file.Close()
		// Each run holds its token until the test lets it go, so how long it holds is the test's to decide.
		command := exec.Command(wrapper, "exec", "--", "sh", "-c",
			"while [ ! -e '"+release+"' ]; do sleep 0.05; done # "+marker)
		command.Env, command.Stdout, command.Stderr = environment, file, file
		if startError := command.Start(); startError != nil {
			test.Fatal(startError)
		}
		for deadline := time.Now().Add(time.Minute); tickets() < want; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				test.Fatalf("%s never joined the line", marker)
			}
		}
		return waiter{command, output}
	}
	read := func(run waiter) string {
		contents, readError := os.ReadFile(run.output)
		if readError != nil {
			test.Fatal(readError)
		}
		return string(contents)
	}
	second := queue("the second run", 1)
	third := queue("the third run", 2)
	first.release()
	named := func() bool {
		output := read(third)
		changed := strings.Index(output, "the token holders changed; now:")
		return strings.Contains(output, "the first holder") && changed >= 0 &&
			strings.Contains(output[changed:], "the second run")
	}
	for deadline := time.Now().Add(time.Minute); !named(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			break
		}
	}
	sawIt := named()
	// Let both runs finish whatever the third said, so neither outlives the test.
	if createError := os.WriteFile(release, nil, 0o644); createError != nil {
		test.Fatal(createError)
	}
	for _, run := range []waiter{second, third} {
		if waitError := run.command.Wait(); waitError != nil {
			test.Fatalf("a queued run failed: %v\n%s", waitError, read(run))
		}
	}
	if !sawIt {
		test.Errorf("the third run did not name the second as the holder while it held the token:\n%s", read(third))
	}
}

// cohere-dev status shows land's lock beside the tokens: who holds it, or free, and who waits for it.
func TestStatusShowsTheLandLock(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("the land lock is a Unix lock")
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	home := privatePool(test)
	land := filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots", "land")
	if createDirectoryError := os.MkdirAll(land, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	status := func() string {
		test.Helper()
		command := exec.Command(wrapper, "status")
		command.Env = append(outsideThePool(os.Environ()), homeVariable+"="+home)
		output, outputError := command.CombinedOutput()
		if outputError != nil {
			test.Fatalf("cohere-dev status: %v\n%s", outputError, output)
		}
		return string(output)
	}
	if output := status(); !strings.Contains(output, "land lock: free") ||
		!strings.Contains(output, "land line: 0 waiting") {
		test.Errorf("status does not show a free land lock:\n%s", output)
	}
	held, takeSlotError := takeSlot(land, 1, "landing 1234abcd from a worktree")
	if takeSlotError != nil || held == nil {
		test.Fatalf("taking the land lock: %v", takeSlotError)
	}
	defer held.release()
	ticket, joinQueueError := joinQueue(land)
	if joinQueueError != nil {
		test.Fatal(joinQueueError)
	}
	defer ticket.leave()
	if output := status(); !strings.Contains(output, "land lock: pid") ||
		!strings.Contains(output, "landing 1234abcd") ||
		!strings.Contains(output, "land line: 1 waiting") {
		test.Errorf("status does not show the land lock's holder and its line:\n%s", output)
	}
}

// A held token whose holder file can't be read says so, rather than that its run has not named itself, which
// is what a holder file not yet written means.
func TestAHeldTokenWhoseHolderCannotBeReadSaysSo(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("a token is a Unix lock")
	}
	slots := test.TempDir()
	held, takeSlotError := takeSlot(slots, 1, "a run whose holder file the test replaces")
	if takeSlotError != nil || held == nil {
		test.Fatalf("taking the token: %v", takeSlotError)
	}
	defer held.release()
	// A directory where the holder file was: reading it fails, and not because it is missing.
	if removeError := os.Remove(held.holder); removeError != nil {
		test.Fatal(removeError)
	}
	if createDirectoryError := os.Mkdir(held.holder, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if holder := heldBy(slots, 1); !strings.HasPrefix(holder, "held, and its holder could not be read: ") {
		test.Errorf("a held token whose holder can't be read reads %q", holder)
	}
}
