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
	type waiter struct {
		command *exec.Cmd
		output  *strings.Builder
	}
	queue := func(marker string, want int) waiter {
		var output strings.Builder
		command := exec.Command(wrapper, "exec", "--", "sh", "-c", "sleep 2 # "+marker)
		command.Env, command.Stdout, command.Stderr = environment, &output, &output
		if startError := command.Start(); startError != nil {
			test.Fatal(startError)
		}
		for deadline := time.Now().Add(time.Minute); tickets() < want; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				test.Fatalf("%s never joined the line", marker)
			}
		}
		return waiter{command, &output}
	}
	second := queue("the second run", 1)
	third := queue("the third run", 2)
	first.release()
	for _, run := range []waiter{second, third} {
		if waitError := run.command.Wait(); waitError != nil {
			test.Fatalf("a queued run failed: %v\n%s", waitError, run.output)
		}
	}
	if output := third.output.String(); !strings.Contains(output, "the first holder") ||
		!strings.Contains(output, "the token holders changed; now:") || !strings.Contains(output, "the second run") {
		test.Errorf("the third run did not name the second as the holder once it took the token:\n%s", output)
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
	held, err := takeSlot(land, 1, "landing 1234abcd from a worktree")
	if err != nil || held == nil {
		test.Fatalf("taking the land lock: %v", err)
	}
	defer held.release()
	ticket, err := joinQueue(land)
	if err != nil {
		test.Fatal(err)
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
