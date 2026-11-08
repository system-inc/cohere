//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/system-inc/cohere/internal/streams"
)

// returnEarly runs the engine as a child and exits with its exit code as soon as the engine writes it to
// the verdict descriptor, leaving the child to finish alone. See execute.
func returnEarly(binaryPath string, arguments []string) (int, error) {
	verdictReader, verdictWriter, pipeError := os.Pipe()
	if pipeError != nil {
		return 0, fmt.Errorf("opening the verdict pipe: %w", pipeError)
	}
	command := exec.Command(binaryPath, arguments...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.ExtraFiles = []*os.File{verdictWriter}
	command.Env = append(os.Environ(), "COHERE_VERDICT_FD=3")

	// A signal sent to this process alone reaches the engine too, as it would have with exec. One from the
	// terminal reaches both already, and a second copy changes nothing.
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	// The engine writes to the terminal itself, so what this process said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	if startError := command.Start(); startError != nil {
		return 0, fmt.Errorf("running %s: %w", binaryPath, startError)
	}
	// Ignored: this process's copy of the write end, never written. A close fails only on a descriptor already
	// closed, and then nothing here holds the pipe open either, which is all the read below needs.
	_ = verdictWriter.Close()
	go func() {
		for received := range signals {
			// Ignored: the engine having exited already is the one way this fails, and then there is nothing
			// left to signal.
			_ = command.Process.Signal(received)
		}
	}()

	verdict := make([]byte, 1)
	// Ignored: any failure to read a byte, its end included, is no verdict, and the engine's own exit status
	// below is the answer.
	if read, _ := verdictReader.Read(verdict); read == 1 {
		os.Exit(streams.Finish(int(verdict[0])))
	}
	// No verdict: the engine ended, or closed the descriptor, without answering. An exit that is not zero is
	// the engine's answer, read from its state below; any other failure leaves no state to read.
	if waitError := command.Wait(); waitError != nil {
		var exitError *exec.ExitError
		if !errors.As(waitError, &exitError) {
			return 0, fmt.Errorf("waiting for %s: %w", binaryPath, waitError)
		}
	}
	signal.Stop(signals)
	if status, isWait := command.ProcessState.Sys().(syscall.WaitStatus); isWait && status.Signaled() {
		// Killed by a signal, so this process dies of the same one, and the caller's shell reports it as it
		// would have reported the engine's death under exec.
		signal.Reset(status.Signal())
		// Ignored: a signal this process can't send itself falls to the exit below, which still fails.
		_ = syscall.Kill(os.Getpid(), status.Signal())
	}
	os.Exit(streams.Finish(command.ProcessState.ExitCode()))
	return 0, nil
}
