// Command cohere-dev is the house's way to run cohere's whole-module tests (#3kr3x59).
//
//	go run ./command/cohere-dev test ./...            every package, through a machine-wide slot
//	go run ./command/cohere-dev test ./internal/edit  one package, at once
//	go run ./command/cohere-dev test --fast           the edit loop: all but the landing gate's own (fast.go)
//	go run ./command/cohere-dev test --full-priority  any of these, not niced, for a timed measurement
//
// A whole-module run builds and links seventy-odd test binaries on every core the machine has, and
// several members starting one together made the machine crawl for all of them. So a run naming ./...
// takes the machine's slot, shared by every checkout on it, and one that finds it taken says whose run
// holds it and waits. Inside a run nothing is held back: Go's package parallelism and
// each test's t.Parallel run at full width, as Kirk asked.
//
// A whole-module run never passes -count=1. Go's test cache skips every package whose inputs did not
// change, which on a one-file change is most of the module, and -count=1 throws that away. A flaky test
// is fixed, not re-run around; name its package to re-run it alone.
//
// Every run is nice 10, and so is everything it starts, so tests use the cores no one else is using and
// yield to a person typing. --full-priority keeps the priority the run was started with, for a measurement.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// slotsVariable overrides how many whole-module runs the machine runs at once.
const slotsVariable = "COHERE_TEST_SLOTS"

// defaultSlots is how many whole-module runs the machine runs at once. Measured on #3kr3x59 with three
// runs started together at the org's load: all at once, none finished (command/cohere passed go test's
// ten minutes in all three); one slot finished two green; two slots finished one.
const defaultSlots = 1

func main() {
	if len(os.Args) < 2 || os.Args[1] != "test" {
		fmt.Fprintln(os.Stderr, "usage: cohere-dev test [--full-priority] [go test flags] [packages] | cohere-dev test --fast [--full-priority] [go test flags]")
		os.Exit(2)
	}
	os.Exit(test(os.Args[2:]))
}

// fullPriorityFlag keeps a run at the priority it was started with, for a timed measurement. Without it a
// run lowers itself to testNiceness first.
const fullPriorityFlag = "--full-priority"

// testNiceness is how nice a run makes itself, and so every go command and test binary it starts. Tests
// compiling and running on every core held the machine at load 168 on 16 cores and made Kirk's typing lag
// (2026-10-05); at nice 10 they still take every idle cycle and yield to what a person is doing.
const testNiceness = 10

// nicenessVariable sets another niceness than testNiceness, from 0 to 19, for whoever wants their tests
// to yield more, and for the test that shows a run is niced whatever priority it was started at.
const nicenessVariable = "COHERE_DEV_NICENESS"

// niceness is how nice a run makes itself: testNiceness, or what nicenessVariable says.
func niceness() (int, error) {
	value := os.Getenv(nicenessVariable)
	if value == "" {
		return testNiceness, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 || parsed > 19 {
		return 0, fmt.Errorf("%s is %q, want a whole number from 0 to 19", nicenessVariable, value)
	}
	return parsed, nil
}

// test runs go test with the arguments given, through a slot when it names the whole module.
func test(arguments []string) int {
	arguments, fullPriority := withoutFlag(arguments, fullPriorityFlag)
	if !fullPriority {
		level, err := niceness()
		if err != nil {
			fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
			return 2
		}
		if err := lowerPriority(level); err != nil {
			fmt.Fprintf(os.Stderr, "cohere-dev: lowering this run's priority: %v\n", err)
			return 1
		}
	}
	if len(arguments) > 0 && arguments[0] == "--fast" {
		return fastTest(arguments[1:])
	}
	if !wholeModule(arguments) {
		return goTest(arguments)
	}
	if countsOnce(arguments) {
		refuseCountOnce()
		return 2
	}

	slots, err := slotCount()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
		return 2
	}
	directory, err := os.UserCacheDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
		return 1
	}
	directory = filepath.Join(directory, "cohere", "test-slots")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
		return 1
	}

	// In line, in arrival order, where a lock holds; see queueTicket.
	var ticket *queueTicket
	if filesLock {
		if ticket, err = joinQueue(directory); err != nil {
			fmt.Fprintf(os.Stderr, "cohere-dev: joining the line for a test slot: %v\n", err)
			return 1
		}
		// Left as soon as a slot is taken, so the next in line moves up while this run tests; this covers
		// every way out before that.
		defer func() {
			if ticket != nil {
				ticket.leave()
			}
		}()
	}

	waitingSince := time.Now()
	announced := false
	lastAhead := -1
	for {
		ahead, waiting := 0, 0
		if ticket != nil {
			if ahead, waiting, err = ticket.place(); err != nil {
				fmt.Fprintf(os.Stderr, "cohere-dev: reading the line for a test slot: %v\n", err)
				return 1
			}
		}
		// Only the first in line, as many as there are slots, may take one, so no later arrival passes them.
		if ahead < slots {
			held, err := takeSlot(directory, slots, arguments)
			if err != nil {
				fmt.Fprintf(os.Stderr, "cohere-dev: taking a test slot: %v\n", err)
				return 1
			}
			if held != nil {
				if ticket != nil {
					ticket.leave()
					ticket = nil
				}
				if announced {
					fmt.Fprintf(os.Stderr, "cohere-dev: took a test slot after waiting %s\n", time.Since(waitingSince).Round(time.Second))
				}
				defer held.release()
				return goTest(arguments)
			}
		}
		if !announced {
			fmt.Fprintf(os.Stderr, "cohere-dev: waiting for a test slot; the machine runs %d whole-module runs at once, and these hold them:\n", slots)
			for _, holder := range holders(directory, slots) {
				fmt.Fprintf(os.Stderr, "  %s\n", holder)
			}
			announced = true
		}
		if ticket != nil && ahead != lastAhead {
			fmt.Fprintf(os.Stderr, "cohere-dev: %s in line of %d waiting\n", ordinal(ahead+1), waiting)
			lastAhead = ahead
		}
		time.Sleep(time.Second)
	}
}

// withoutFlag returns the arguments with every copy of flag removed, and whether there was one, so a flag of
// this wrapper's never reaches go test.
func withoutFlag(arguments []string, flag string) ([]string, bool) {
	kept := make([]string, 0, len(arguments))
	found := false
	for _, argument := range arguments {
		if argument == flag {
			found = true
			continue
		}
		kept = append(kept, argument)
	}
	return kept, found
}

// wholeModule reports whether the arguments name every package, the run that takes a slot.
func wholeModule(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "./..." || argument == "..." {
			return true
		}
	}
	return false
}

// refuseCountOnce says why a run of nearly every package, through the slot or the fast tier, never passes
// -count=1.
func refuseCountOnce() {
	fmt.Fprintln(os.Stderr, "cohere-dev: a whole-module run never passes -count=1: it re-runs every package whose "+
		"inputs did not change, which Go's test cache would have skipped. Fix a flaky test rather than re-run "+
		"around it, and name a package to re-run it alone: go test -count=1 ./its/package")
}

// countsOnce reports whether the arguments turn Go's test cache off with -count=1.
func countsOnce(arguments []string) bool {
	for index, argument := range arguments {
		switch {
		case argument == "-count=1" || argument == "--count=1":
			return true
		case (argument == "-count" || argument == "--count") && index+1 < len(arguments) && arguments[index+1] == "1":
			return true
		}
	}
	return false
}

func slotCount() (int, error) {
	value := os.Getenv(slotsVariable)
	if value == "" {
		return defaultSlots, nil
	}
	slots, err := strconv.Atoi(value)
	if err != nil || slots < 1 {
		return 0, fmt.Errorf("%s is %q, want a whole number of at least 1", slotsVariable, value)
	}
	return slots, nil
}

// goTest runs go test with the caller's terminal and returns its exit code.
func goTest(arguments []string) int {
	environment, uncovered, err := testEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
		return 2
	}
	command := exec.Command("go", append([]string{"test"}, arguments...)...)
	command.Env = environment
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	defer printUncovered(os.Stderr, uncovered)
	if err := command.Run(); err != nil {
		if exited, ok := err.(*exec.ExitError); ok {
			return exited.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "cohere-dev: running go test: %v\n", err)
		return 1
	}
	return 0
}

// holderLine describes the run holding a slot: who, where, since when and what it ran.
func holderLine(arguments []string) string {
	directory, _ := os.Getwd()
	return fmt.Sprintf("pid %d in %s since %s: go test %s", os.Getpid(), directory,
		time.Now().Format("15:04:05"), strings.Join(arguments, " "))
}

// holders reads who holds each taken slot. A slot that is free says nothing.
func holders(directory string, slots int) []string {
	lines := []string{}
	for slot := 1; slot <= slots; slot++ {
		contents, err := os.ReadFile(holderPath(directory, slot))
		if err == nil && len(contents) > 0 {
			lines = append(lines, strings.TrimSpace(string(contents)))
		}
	}
	return lines
}

func lockPath(directory string, slot int) string {
	return filepath.Join(directory, fmt.Sprintf("slot-%d.lock", slot))
}

func holderPath(directory string, slot int) string {
	return filepath.Join(directory, fmt.Sprintf("slot-%d.holder", slot))
}

// heldSlot is a slot this process holds until release, or until it exits.
type heldSlot struct {
	lock   *os.File
	holder string
}

func (held *heldSlot) release() {
	os.Remove(held.holder)
	unlockFile(held.lock)
	held.lock.Close()
}

// takeSlot takes the first free slot, or returns nil when every slot is held.
func takeSlot(directory string, slots int, arguments []string) (*heldSlot, error) {
	for slot := 1; slot <= slots; slot++ {
		lock, err := os.OpenFile(lockPath(directory, slot), os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return nil, err
		}
		taken, err := tryLockFile(lock)
		if err != nil {
			lock.Close()
			return nil, err
		}
		if !taken {
			lock.Close()
			continue
		}
		holder := holderPath(directory, slot)
		if err := os.WriteFile(holder, []byte(holderLine(arguments)+"\n"), 0o644); err != nil {
			unlockFile(lock)
			lock.Close()
			return nil, err
		}
		return &heldSlot{lock: lock, holder: holder}, nil
	}
	return nil, nil
}
