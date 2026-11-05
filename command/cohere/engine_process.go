package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// engineWaitDelay bounds how long Wait may wait on the engine's pipes after the engine has ended. Killing
// the engine's process group closes them at once; this is the backstop for a descendant that left the
// group, so a stray holding a pipe open can never hold the front door open with it.
const engineWaitDelay = 2 * time.Second

// runEngineBinary runs an engine, renders its stdout record by record as it arrives, passes its
// stderr through untouched, and returns the exit code the front door owns.
//
// Records are rendered as they stream so a slow types phase shows its fix line first, as a TypeScript
// run does. A record that breaks the contract stops the run there: the engine is killed, the phase
// line says what was not reached, and the error says the engine is broken.
func runEngineBinary(binaryPath string, arguments []string, run *engineRun, standardError io.Writer) (int, error) {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(interrupts)
	return runEngineBinaryUntil(binaryPath, arguments, run, standardError, interrupts)
}

// runEngineBinaryUntil is runEngineBinary with the signals to forward handed in, so a test can deliver
// one without signalling its own process.
//
// The engine runs in a process group of its own, so ending it ends everything it started: a build or a
// compiler it spawned would otherwise outlive it, still running and still holding its output pipes
// open. A group of its own is also out of the terminal's reach, so an interrupt the terminal sends to
// cohere is passed on to the engine's group here, every time it arrives, rather than left to orphan it.
func runEngineBinaryUntil(
	binaryPath string,
	arguments []string,
	run *engineRun,
	standardError io.Writer,
	interrupts <-chan os.Signal,
) (int, error) {
	command := exec.Command(binaryPath, arguments...)
	if len(run.engineEnvironment) > 0 {
		command.Env = append(os.Environ(), run.engineEnvironment...)
	}
	command.Stderr = standardError
	startInOwnProcessGroup(command)
	command.WaitDelay = engineWaitDelay
	standardOutput, stdoutPipeError := command.StdoutPipe()
	if stdoutPipeError != nil {
		return 1, fmt.Errorf("connecting to %s's output: %w", run.language.theEngine(), stdoutPipeError)
	}
	if startError := command.Start(); startError != nil {
		return 1, fmt.Errorf("starting %s %s: %w", run.language.theEngine(), binaryPath, startError)
	}

	// With Setpgid the engine leads its group, so the group's id is the engine's process id.
	engineGroup := command.Process.Pid
	runEnded := make(chan struct{})
	defer close(runEnded)
	go func() {
		for {
			select {
			case received := <-interrupts:
				signalProcessGroup(engineGroup, received)
			case <-runEnded:
				return
			}
		}
	}()

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
		killEngineGroup(engineGroup, command)
		run.writeUnfinished(run.language.theEngine() + " broke its contract")
		return 1, fmt.Errorf("%s is broken, so nothing it reported can be trusted: %w",
			run.language.theEngine(), protocolError)
	}

	waitError := command.Wait()
	exitCode, ended := describeExit(command.ProcessState, waitError)
	return run.finish(exitCode, ended)
}

// killEngineGroup kills every process in the engine's group and waits for the engine.
//
// One kill is not enough: a child the engine forks at the instant the group is signalled can miss the
// signal, and it would outlive the engine holding its pipes. So the group is signalled again until the
// engine is reaped and its pipes are closed. Signalling it again is safe, because a process id is never
// reused while a process group of that id still has members.
func killEngineGroup(engineGroup int, command *exec.Cmd) {
	killProcessGroup(engineGroup)
	waited := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(waited)
	}()
	for {
		select {
		case <-waited:
			return
		case <-time.After(20 * time.Millisecond):
			killProcessGroup(engineGroup)
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
