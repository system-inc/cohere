//go:build unix

package process_reading

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// unheldProcess is an id no process can hold: past macOS's largest, 99,998, and Linux's, 4,194,304.
const unheldProcess = 1 << 30

// A process this test starts reads back with the arguments it was started with, its executable's name first, and
// the niceness this test gives it, while it runs. Start returns once the child has run exec, so its command line is
// the sleep's from the first read.
func TestAStartedProcessReadsBackItsCommandAndNiceness(test *testing.T) {
	test.Parallel()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		test.Skip("the command line is read only on macOS and Linux")
	}
	sleeper := exec.Command("sleep", "100000")
	if startError := sleeper.Start(); startError != nil {
		test.Fatal(startError)
	}
	test.Cleanup(func() {
		// Ignored: the sleep is this test's own child, ended here, and its wait reports the kill.
		_ = sleeper.Process.Kill()
		_ = sleeper.Wait() // Ignored: it reports the kill above, which is how the sleep is meant to end.
	})
	command, commandError := Command(sleeper.Process.Pid)
	if commandError != nil {
		test.Fatal(commandError)
	}
	if !slices.Equal(command, []string{"sleep", "100000"}) {
		test.Fatalf("the command line reads %q, want sleep 100000", command)
	}
	own, ownError := Niceness(os.Getpid())
	if ownError != nil {
		test.Fatal(ownError)
	}
	// Lowered by seven from this test's own, which any process may do to its child.
	if setError := syscall.Setpriority(syscall.PRIO_PROCESS, sleeper.Process.Pid, min(own+7, 19)); setError != nil {
		test.Fatal(setError)
	}
	nice, niceError := Niceness(sleeper.Process.Pid)
	if niceError != nil {
		test.Fatal(niceError)
	}
	if want := min(own+7, 19); nice != want {
		test.Errorf("the sleep runs at nice %d, want %d, this test's %d and seven", nice, want, own)
	}
	description, describeError := Describe(sleeper.Process.Pid)
	if describeError != nil {
		test.Fatal(describeError)
	}
	if description.Parent != os.Getpid() || !slices.Equal(description.Command, []string{"sleep", "100000"}) {
		test.Errorf("the sleep is described as %+v, want this test as its parent and sleep 100000", description)
	}
	// Started no earlier than this test and no later than now, an order and never a margin: Linux counts a start to
	// the hundredth of a second, so two starts in one tick read the same.
	this, thisError := Describe(os.Getpid())
	if thisError != nil {
		test.Fatal(thisError)
	}
	if description.Started.Before(this.Started) || description.Started.After(time.Now()) {
		test.Errorf("the sleep is described as started at %s, want between this test's start %s and now",
			description.Started, this.Started)
	}
	if said := description.String(); !strings.HasPrefix(said, strconv.Itoa(os.Getpid())+" ") ||
		!strings.HasSuffix(said, " sleep 100000") {
		test.Errorf("the description reads %q, want ps's parent, start and command", said)
	}
}

// An id no process holds is an error from both readers, never an empty command line or a nice value of 0.
func TestAnIdNoProcessHoldsIsAnError(test *testing.T) {
	test.Parallel()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		test.Skip("the command line is read only on macOS and Linux")
	}
	if command, commandError := Command(unheldProcess); !errors.Is(commandError, ErrNoProcess) {
		test.Errorf("an id no process holds read the command line %q with %v, want ErrNoProcess", command,
			commandError)
	}
	if nice, niceError := Niceness(unheldProcess); niceError == nil {
		test.Errorf("an id no process holds read nice %d with no error", nice)
	}
	if description, describeError := Describe(unheldProcess); !errors.Is(describeError, ErrNoProcess) {
		test.Errorf("an id no process holds was described as %+v with %v, want ErrNoProcess", description,
			describeError)
	}
}
