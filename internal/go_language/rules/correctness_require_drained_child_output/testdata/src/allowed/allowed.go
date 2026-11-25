package allowed

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
)

func use(...any) {}

func keep(io.Reader) {}

// 5c5833e's fixed form: the table is read to its end, then the child is waited on.
func sampleProcesses() ([]byte, error) {
	command := exec.Command("ps", "-eo", "pid,ppid,%cpu")
	stdout, pipeError := command.StdoutPipe()
	if pipeError != nil {
		return nil, pipeError
	}
	if startError := command.Start(); startError != nil {
		return nil, startError
	}
	output, readError := io.ReadAll(stdout)
	if waitError := command.Wait(); waitError != nil {
		return nil, waitError
	}
	return output, readError
}

// A goroutine copies the pipe while the function waits.
func concurrentDrain(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	go func() {
		_, _ = io.Copy(os.Stdout, stdout)
	}()
	return command.Wait()
}

// Each branch drains the pipe before the wait.
func drainOnBothBranches(command *exec.Cmd, verbose bool) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	if verbose {
		_, _ = io.Copy(os.Stdout, stdout)
	} else {
		_, _ = io.Copy(io.Discard, stdout)
	}
	return command.Wait()
}

// A scanner reads the pipe line by line to its end, then the child is waited on.
func scanLines(command *exec.Cmd) ([]string, error) {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	lines := []string{}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, command.Wait()
}

// A buffer exec fills itself is no pipe of the caller's.
func buffered() (string, error) {
	var buffer bytes.Buffer
	command := exec.Command("date")
	command.Stdout = &buffer
	runError := command.Run()
	return buffer.String(), runError
}

// Output reads the child's stdout to its end itself.
func output() ([]byte, error) {
	return exec.Command("date").Output()
}

// An inherited stream, and a discarded one, are no pipes.
func inheritedAndDiscarded() error {
	command := exec.Command("date")
	command.Stdout = os.Stdout
	command.Stderr = nil
	return command.Run()
}

// A pipe handed to a helper is read where the rule cannot see, so it counts as drained.
func handedOn(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	keep(stdout)
	return command.Wait()
}

type reader struct{ source io.Reader }

// A pipe stored in a field is read where the rule cannot see.
func stored(command *exec.Cmd, into *reader) error {
	stdout, _ := command.StdoutPipe()
	into.source = stdout
	_ = command.Start()
	return command.Wait()
}

// No wait on any path: the zombie companion's case, not this rule's.
func neverWaited(command *exec.Cmd) io.Reader {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	use(command)
	return stdout
}

// The command variable is reassigned, so the wait may be on another child.
func reassignedCommand() error {
	command := exec.Command("ps")
	stdout, _ := command.StdoutPipe()
	command = exec.Command("date")
	runError := command.Run()
	use(stdout)
	return runError
}

// The pipe's variable is written again, so it may hold another reader by the wait.
func reassignedPipe(command *exec.Cmd, other io.ReadCloser) error {
	stdout, _ := command.StdoutPipe()
	stdout = other
	_ = command.Start()
	waitError := command.Wait()
	use(stdout)
	return waitError
}

// A pipe dropped where it is made is no variable to follow.
func droppedPipe(command *exec.Cmd) error {
	_, _ = command.StdoutPipe()
	return command.Run()
}

// The pipe belongs to one command and the wait to another.
func otherCommand(first *exec.Cmd, second *exec.Cmd) error {
	stdout, _ := first.StdoutPipe()
	_ = first.Start()
	_ = second.Run()
	_, _ = io.ReadAll(stdout)
	return first.Wait()
}

// A command reached through a call's result is one no later line can name again.
func commandOf(commands []*exec.Cmd) error {
	stdout, _ := commands[0].StdoutPipe()
	_ = commands[0].Start()
	waitError := commands[0].Wait()
	use(stdout)
	return waitError
}

// A deferred wait runs on the way out, after the read.
func deferredWait(command *exec.Cmd) ([]byte, error) {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	defer command.Wait()
	return io.ReadAll(stdout)
}

// A wait on a path that has already left never runs.
func waitAfterExit(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	os.Exit(1)
	waitError := command.Wait()
	use(stdout)
	return waitError
}

// A wait inside a function literal is judged on the literal's own graph, where no pipe is made.
func waitInLiteral(command *exec.Cmd) func() error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	wait := func() error { return command.Wait() }
	_, _ = io.ReadAll(stdout)
	return wait
}

// The read and the wait in one statement count as the read.
func readAndWaitTogether(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	return errors.Join(copyAll(stdout), command.Wait())
}

func copyAll(source io.Reader) error {
	_, copyError := io.Copy(io.Discard, source)
	return copyError
}
