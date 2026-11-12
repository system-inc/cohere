package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/system-inc/cohere/internal/streams"
)

// A bounded run (#fp1td9e): a mutant or probe in code that starts processes runs under a process limit and a
// deadline, the standing rule since a review mutant with its guard removed forked its own binary about 7,700
// times on 2026-11-10 and filled Kirk's process table, so fork failed for every member (#excff9x).
//
//	cohere-dev test --bounded ./command/cohere
//	cohere-dev exec --bounded-margin 64 --bounded-deadline 30s -- ./probe
//
// The margin caps the run's own process group: a watchdog counts the group's processes every few milliseconds
// (groupProcessCount) and stops the whole group the moment it holds more than the margin, saying so. It counts
// the group, not the account, because the account's total moves with everyone else's work: on Home, Loom's
// Linux box, the account ran 2,904 threads with gates coming and going, and a margin taken against that total
// let a spawner start all 400 of its sleeps under a margin of 32 (#acdf5mc). The group's count belongs to this
// run alone.
//
// Behind the watchdog stands the kernel's process limit, set wider, as a backstop for what the watchdog can't
// see: a process that leaves the group, and the forks a storm makes between two of its looks. It is the
// account's count when the run begins plus backstopFactor times the margin. The kernel counts every process
// the account owns, so a fixed number is wrong (`ulimit -u 512` refused every fork on a Mac where the account
// owned about 850), and Linux counts threads, so its factor is larger.
//
// The deadline stops the whole process group, not only the command: the command runs in a group of its own,
// its group is sent `SIGTERM` when the deadline passes and `SIGKILL` a few seconds later, and whatever is left
// in the group when the command ends is stopped too, so a spawner that exits before its children leaves
// nothing behind. An interrupt or a termination of cohere-dev itself is passed to the group, since a group of
// its own no longer hears the terminal. A bounded command reads no terminal: its standard input is empty, as
// a group the terminal is not waiting on would be stopped by reading it.
//
// What the group cannot hold is a process that leaves it, by starting a session or a group of its own; the
// backstop still counts that one.

// boundedFlag bounds a run with the default margin and deadline.
const boundedFlag = "--bounded"

// boundedMarginFlag bounds a run and sets its margin: how many processes its group may hold at once.
const boundedMarginFlag = "--bounded-margin"

// boundedDeadlineFlag bounds a run and sets its deadline, in Go's duration form: 30s, 5m.
const boundedDeadlineFlag = "--bounded-deadline"

// defaultBoundedMargin is the processes a bounded run's group may hold at once: 512, about one fifteenth of the
// 7,700 the storm reached, and room for a test of command/cohere, whose tests start builds and runs of cohere two
// at a time.
const defaultBoundedMargin = 512

// defaultBoundedDeadline is Go's own per-binary test timeout of 10 minutes, with 5 more for the build before
// it, which the deadline covers and Go's timeout does not, so a bounded run of a package's tests is not stopped
// sooner than its go test would stop it. command/cohere's tests took 5m04s bounded, built and run, on
// 2026-11-11.
const defaultBoundedDeadline = 15 * time.Minute

// boundedGrace is how long a group has between `SIGTERM` and `SIGKILL`, at the deadline or an interrupt.
const boundedGrace = 5 * time.Second

// boundedPoll is how often the watchdog counts the group, at the least: a count that takes longer waits twice its
// own time before the next, so the watchdog never takes more than a third of a core.
const boundedPoll = 10 * time.Millisecond

// marginExitCode is what a run whose group passed its margin exits with, one below the deadline's.
const marginExitCode = 123

// deadlineExitCode is what a run stopped at its deadline exits with, coreutils timeout's code, which a script
// that knew the recipe already reads.
const deadlineExitCode = 124

// bound is a bounded run's process limit and deadline. A nil bound is an unbounded run.
type bound struct {
	margin   int
	deadline time.Duration
	// until is when the deadline passes, set by begin: one deadline for every command of the run, as the fast
	// tier's go list and go test share one.
	until time.Time
}

// withoutBoundedFlags takes this wrapper's bounded flags out of the arguments, so none reaches go, and returns
// the bound they ask for, nil when they ask for none. A margin or a deadline bounds the run on its own. leading
// reads only the flags before the first argument that is not one of them, for exec, whose command's own
// flags are its own. Either way, reading stops at "--" or go test's -args, which pass what follows on.
func withoutBoundedFlags(arguments []string, leading bool) ([]string, *bound, error) {
	kept := make([]string, 0, len(arguments))
	var asked *bound
	ask := func() *bound {
		if asked == nil {
			asked = &bound{margin: defaultBoundedMargin, deadline: defaultBoundedDeadline}
		}
		return asked
	}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--" || argument == "-args" || argument == "--args" {
			return append(kept, arguments[index:]...), asked, nil
		}
		name, value, hasValue := strings.Cut(argument, "=")
		if name != boundedFlag && name != boundedMarginFlag && name != boundedDeadlineFlag {
			if leading {
				return append(kept, arguments[index:]...), asked, nil
			}
			kept = append(kept, argument)
			continue
		}
		if name == boundedFlag {
			if hasValue {
				return nil, nil, fmt.Errorf("%s takes no value; set the margin with %s and the deadline with %s",
					boundedFlag, boundedMarginFlag, boundedDeadlineFlag)
			}
			ask()
			continue
		}
		if !hasValue {
			if index+1 == len(arguments) {
				return nil, nil, fmt.Errorf("%s needs a value", name)
			}
			index++
			value = arguments[index]
		}
		if name == boundedMarginFlag {
			margin, parseError := strconv.Atoi(value)
			if parseError != nil || margin < 1 {
				return nil, nil, fmt.Errorf("%s is %q, want a whole number of processes of at least 1", name, value)
			}
			ask().margin = margin
			continue
		}
		deadline, parseError := time.ParseDuration(value)
		if parseError != nil || deadline <= 0 {
			return nil, nil, fmt.Errorf("%s is %q, want a duration above zero, such as 30s or 5m", name, value)
		}
		ask().deadline = deadline
	}
	return kept, asked, nil
}

// begin holds this process, and so everything it starts, to the backstop, the account's process count now plus
// backstopFactor times the margin, and starts the deadline. It says what it set, since the backstop depends on
// the machine at that moment. The margin itself is held by run's watchdog. A nil bound does nothing.
func (limit *bound) begin() error {
	if limit == nil {
		return nil
	}
	// The kernel holds root to no process limit, so a bounded run as root would be bounded by nothing while
	// saying it was.
	if os.Getuid() == 0 {
		return errors.New("a bounded run can't run as root: the kernel holds root to no process limit")
	}
	count, countError := accountProcessCount()
	if countError != nil {
		return fmt.Errorf("counting this account's processes for a bounded run: %w", countError)
	}
	backstop := count + backstopFactor*limit.margin
	if limitError := holdProcessLimit(backstop); limitError != nil {
		return fmt.Errorf("setting a bounded run's backstop process limit to %d: %w", backstop, limitError)
	}
	limit.until = time.Now().Add(limit.deadline)
	fmt.Fprintf(streams.Errors,
		"cohere-dev: bounded: this run's process group may hold %d processes and stops at %s; this account has "+
			"%d %s, and the backstop limit is %d\n",
		limit.margin, limit.deadline, count, processNoun, backstop)
	return nil
}

// runAttached runs a command with the caller's terminal in the environment given, under the bound when there
// is one, and returns its exit code.
func runAttached(limit *bound, environment []string, name string, arguments []string) int {
	what := name
	if name == "go" && len(arguments) > 0 {
		what = "go " + arguments[0]
	}
	command := exec.Command(name, arguments...)
	command.Env = environment
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	// The command writes to the terminal itself, so what this run said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	if limit == nil {
		return attachedExitCode(what, command.Run())
	}
	// A bounded command reads no terminal (the file's opening comment says why) and leads a group of its own.
	command.Stdin = nil
	command.SysProcAttr = ownGroup()
	return attachedExitCode(what, limit.run(what, command))
}

// run runs a bounded command runAttached made, and returns its wait's error: the group is stopped when it holds
// more than the margin, at the deadline, on an interrupt, and once the command ends.
func (limit *bound) run(name string, command *exec.Cmd) error {
	// Asked for before the start, so an interrupt that arrives while the command starts is held for the group.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if startError := command.Start(); startError != nil {
		return startError
	}
	// A group of its own takes its leader's pid as its id.
	group := command.Process.Pid
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	deadline := time.NewTimer(time.Until(limit.until))
	defer deadline.Stop()
	passed := make(chan int, 1)
	stopWatching := make(chan struct{})
	defer close(stopWatching)
	go watchGroup(group, limit.margin, passed, stopWatching)

	stopGroup := func(first syscall.Signal) error {
		signalGroup(group, first)
		select {
		case waitError := <-finished:
			return waitError
		case <-time.After(boundedGrace):
			signalGroup(group, syscall.SIGKILL)
			return <-finished
		}
	}
	var waitError error
	select {
	case waitError = <-finished:
	case received := <-signals:
		forwarded, isSignal := received.(syscall.Signal)
		if !isSignal {
			forwarded = syscall.SIGTERM
		}
		waitError = stopGroup(forwarded)
	case held := <-passed:
		// The group is past its margin, a storm in the making, so it is stopped at once, with no grace.
		signalGroup(group, syscall.SIGKILL)
		// The command was killed, so its wait's error is that kill, and the margin is what is said.
		<-finished
		waitError = &marginPassed{name: name, margin: limit.margin, held: held}
	case <-deadline.C:
		// Ignored: the command was stopped, so its wait's error is that stop, and the deadline is what is said.
		_ = stopGroup(syscall.SIGTERM)
		waitError = &deadlinePassed{name: name, deadline: limit.deadline}
	}
	// What the command left in its group, children it started and did not wait for, is stopped with it.
	if groupAlive(group) {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s left processes in its group; stopping them\n", name)
		signalGroup(group, syscall.SIGKILL)
	}
	return waitError
}

// watchGroup counts a group's processes until stopped, and sends the count on passed, once, when it is over the
// margin. A count that fails is taken as no count, since the backstop still holds.
func watchGroup(group int, margin int, passed chan<- int, stop <-chan struct{}) {
	for {
		started := time.Now()
		if held, countError := groupProcessCount(group); countError == nil && held > margin {
			passed <- held
			return
		}
		wait := max(boundedPoll, 2*time.Since(started))
		select {
		case <-stop:
			return
		case <-time.After(wait):
		}
	}
}

// attachedExitCode is the code a command run attached to the terminal ends this run with: its own exit code, the
// deadline's code when its bounded run's deadline stopped it, said, and 1, said, when it could not run.
func attachedExitCode(name string, runError error) int {
	if runError == nil {
		return 0
	}
	var passed *deadlinePassed
	if errors.As(runError, &passed) {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", passed)
		return deadlineExitCode
	}
	var overMargin *marginPassed
	if errors.As(runError, &overMargin) {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", overMargin)
		return marginExitCode
	}
	var exited *exec.ExitError
	if errors.As(runError, &exited) {
		return exited.ExitCode()
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: running %s: %v\n", name, runError)
	return 1
}
