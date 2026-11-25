package lookalike

import (
	"io"
	"os"
	"os/exec"
)

func use(...any) {}

// A type of this package with StdoutPipe, StderrPipe, Wait and Run methods is not exec.Cmd.
type runner struct{}

func (runner) StdoutPipe() (io.ReadCloser, error) { return nil, nil }
func (runner) StderrPipe() (io.ReadCloser, error) { return nil, nil }
func (runner) Start() error                       { return nil }
func (runner) Wait() error                        { return nil }
func (runner) Run() error                         { return nil }

func ownRunner() error {
	var current runner
	stdout, _ := current.StdoutPipe()
	stderr, _ := current.StderrPipe()
	_ = current.Start()
	waitError := current.Wait()
	runError := current.Run()
	use(stdout, stderr)
	return errorsOf(waitError, runError)
}

func errorsOf(first error, second error) error {
	if first != nil {
		return first
	}
	return second
}

// An os.Pipe handed to a command as its Stdout is a file the child writes to, not one exec hands back.
func filePipe() error {
	reading, writing, _ := os.Pipe()
	command := exec.Command("date")
	command.Stdout = writing
	waitError := command.Run()
	use(reading)
	return waitError
}

// A type embedding exec.Cmd whose own Wait shadows exec's is not exec's Wait.
type wrapped struct{ *exec.Cmd }

func (wrapped) Wait() error { return nil }

func shadowedWait(current wrapped) error {
	stdout, _ := current.StdoutPipe()
	_ = current.Start()
	waitError := current.Wait()
	use(stdout)
	return waitError
}
