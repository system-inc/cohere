package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/system-inc/cohere/internal/command_line/streams"
)

// contractWaitDelay bounds how long Wait may wait on the child's pipes after the child has ended. Killing
// the child's process group closes them at once; this is the backstop for a descendant that left the
// group, so a stray holding a pipe open can never hold the front door open with it.
const contractWaitDelay = 2 * time.Second

// runContractBinary runs a child, renders its stdout record by record as it arrives, passes its
// stderr through untouched, and returns the exit code the front door owns.
//
// Records are rendered as they stream so a slow types phase shows its fix line first, as a TypeScript
// run does. A record that breaks the contract stops the run there: the child is killed, the phase
// line says what was not reached, and the error says the child is broken.
func runContractBinary(binaryPath string, arguments []string, run *contractRun, standardError io.Writer) (int, error) {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(interrupts)
	return runContractBinaryUntil(binaryPath, arguments, run, standardError, interrupts)
}

// runContractBinaryUntil is runContractBinary with the signals to forward handed in, so a test can deliver
// one without signalling its own process.
//
// The child runs in a process group of its own, so ending it ends everything it started: a build or a
// compiler it spawned would otherwise outlive it, still running and still holding its output pipes
// open. A group of its own is also out of the terminal's reach, so an interrupt the terminal sends to
// cohere is passed on to the child's group here, every time it arrives, rather than left to orphan it.
func runContractBinaryUntil(
	binaryPath string,
	arguments []string,
	run *contractRun,
	standardError io.Writer,
	interrupts <-chan os.Signal,
) (int, error) {
	command := exec.Command(binaryPath, arguments...)
	if len(run.childEnvironment) > 0 {
		command.Env = append(os.Environ(), run.childEnvironment...)
	}
	command.Stderr = standardError
	startInOwnProcessGroup(command)
	command.WaitDelay = contractWaitDelay
	standardOutput, stdoutPipeError := command.StdoutPipe()
	if stdoutPipeError != nil {
		return 1, fmt.Errorf("connecting to %s's output: %w", run.language.programName(), stdoutPipeError)
	}
	// The child's stderr is the terminal's, written by the child itself, so what this run said first is
	// delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	if startError := command.Start(); startError != nil {
		return 1, fmt.Errorf("starting %s %s: %w", run.language.programName(), binaryPath, startError)
	}

	// With Setpgid the child leads its group, so the group's id is the child's process id.
	childGroup := command.Process.Pid
	defer forwardInterrupts(childGroup, interrupts)()

	scanner := bufio.NewScanner(standardOutput)
	// A finding's message is bounded by what a rule writes, not by a line length, so the limit is far
	// past any real record and a line past it is still refused loudly rather than split.
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)

	var protocolError error
	for scanner.Scan() {
		if protocolError = run.accept(scanner.Bytes()); protocolError != nil {
			break
		}
	}
	if protocolError == nil && scanner.Err() != nil {
		protocolError = fmt.Errorf("reading its output: %w", scanner.Err())
	}

	if protocolError != nil {
		// Killed rather than drained: nothing it writes after breaking the contract can be trusted, and
		// waiting for it to finish would make the reader wait for output that will be discarded. The
		// whole group, because a child it started would keep running and keep the pipes open.
		killContractGroup(childGroup, command)
		run.writeUnfinished(run.language.programName() + " broke its contract")
		return 1, fmt.Errorf("%s is broken, so nothing it reported can be trusted: %w",
			run.language.programName(), protocolError)
	}

	waitError := command.Wait()
	exitCode, ended := describeExit(command.ProcessState, waitError)
	return run.finish(exitCode, ended)
}

// forwardInterrupts passes each signal interrupts delivers on to the child's process group, which is out of the
// terminal's reach, until the stop it returns is called.
func forwardInterrupts(childGroup int, interrupts <-chan os.Signal) (stop func()) {
	runEnded := make(chan struct{})
	go func() {
		for {
			select {
			case received := <-interrupts:
				signalProcessGroup(childGroup, received)
			case <-runEnded:
				return
			}
		}
	}()
	return func() { close(runEnded) }
}

// runContractForText runs a child whose standard output is a file's text rather than records: the editor's save
// (go_stdin.go), with input on its standard input. The text is returned whole once the child has ended, never
// streamed, so a save is handed all of it or none; its stderr is passed through untouched. It runs as runContractBinary
// runs a child, in a process group of its own with the terminal's interrupts passed on, and environment is what its
// process gets beyond this one's, each `NAME=value`.
func runContractForText(
	binaryPath string,
	arguments []string,
	environment []string,
	input []byte,
	standardError io.Writer,
) (text []byte, exitCode int, ended string, runError error) {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(interrupts)

	command := exec.Command(binaryPath, arguments...)
	if len(environment) > 0 {
		command.Env = append(os.Environ(), environment...)
	}
	command.Stdin = bytes.NewReader(input)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = standardError
	startInOwnProcessGroup(command)
	command.WaitDelay = contractWaitDelay
	// Anything this run said first reaches stderr before the child's own words.
	streams.Show(streams.Errors)
	if startError := command.Start(); startError != nil {
		return nil, 1, "", fmt.Errorf("starting the child %s: %w", binaryPath, startError)
	}
	defer forwardInterrupts(command.Process.Pid, interrupts)()
	waitError := command.Wait()
	exitCode, ended = describeExit(command.ProcessState, waitError)
	return output.Bytes(), exitCode, ended, nil
}

// killContractGroup kills every process in the child's group and waits for the child.
//
// One kill is not enough: a child the child forks at the instant the group is signalled can miss the
// signal, and it would outlive the child holding its pipes. So the group is signalled again until the
// child is reaped and its pipes are closed. Signalling it again is safe, because a process id is never
// reused while a process group of that id still has members.
func killContractGroup(childGroup int, command *exec.Cmd) {
	killProcessGroup(childGroup)
	waited := make(chan struct{})
	go func() {
		// Ignored: the child was killed for breaking its contract, which is the error the run returns; this
		// wait only reaps it, and how a killed child ended says nothing more.
		_ = command.Wait()
		close(waited)
	}()
	for {
		select {
		case <-waited:
			return
		case <-time.After(20 * time.Millisecond):
			killProcessGroup(childGroup)
		}
	}
}

// describeExit turns how a process ended into an exit code and the words for it.
//
// A process killed by a signal has no exit code, and Go reports -1 for it. Printing "exited -1" would
// state a number the process never chose, so a signal is named as a signal.
func describeExit(state *os.ProcessState, waitError error) (int, string) {
	if state == nil {
		return -1, fmt.Sprintf("could not be waited for (%v)", waitError)
	}
	if status, isUnix := state.Sys().(syscall.WaitStatus); isUnix && status.Signaled() {
		return -1, fmt.Sprintf("was killed by %s", status.Signal())
	}
	var exitError *exec.ExitError
	if waitError != nil && !errors.As(waitError, &exitError) {
		return -1, fmt.Sprintf("could not be waited for (%v)", waitError)
	}
	return state.ExitCode(), fmt.Sprintf("exited %d", state.ExitCode())
}
