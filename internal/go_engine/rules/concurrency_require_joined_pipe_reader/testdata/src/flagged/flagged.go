package flagged

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"sync"
)

// os/exec's own example of the mistake: a goroutine copies the pipe, and Wait closes it under the copy.
func copyThenWait(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	go io.Copy(os.Stdout, stdout)
	return command.Wait() // want `^A goroutine reads the stdout pipe, and .Wait. is reached on some path here before that goroutine is joined`
}

// A literal with nothing to join it by.
func literalReader(command *exec.Cmd) ([]string, error) {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	lines := []string{}
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
	}()
	waitError := command.Wait() // want `stdout pipe`
	return lines, waitError
}

// The WaitGroup is waited on, but after the child.
func joinedTooLate(command *exec.Cmd) error {
	stderr, _ := command.StderrPipe()
	_ = command.Start()
	var group sync.WaitGroup
	var output bytes.Buffer
	group.Add(1)
	go func() {
		defer group.Done()
		_, _ = io.Copy(&output, stderr)
	}()
	waitError := command.Wait() // want `^A goroutine reads the stderr pipe`
	group.Wait()
	return waitError
}

// A deferred Wait on the group runs on the way out, after the child's.
func deferredJoin(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		_, _ = io.Copy(io.Discard, stdout)
	}()
	defer group.Wait()
	return command.Wait() // want `stdout pipe`
}

// The channel is received on one branch only, so the other waits on the child first.
func joinedOnOneBranch(command *exec.Cmd, patient bool) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(os.Stdout, stdout)
	}()
	if patient {
		<-done
	}
	return command.Wait() // want `stdout pipe`
}

// Run waits too, and the goroutine started before it is never joined.
func runWithReader(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	go io.Copy(os.Stdout, stdout)
	return command.Run() // want `stdout pipe, and .Run. is reached`
}

// A WaitGroup's Go starts the reader, and the group is waited on only after the child.
func groupGoJoinedLate(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	var group sync.WaitGroup
	group.Go(func() {
		_, _ = io.Copy(io.Discard, stdout)
	})
	waitError := command.Wait() // want `stdout pipe`
	group.Wait()
	return waitError
}

// A writer reached through an interface is another package's, and joins nothing.
func throughAnInterface(command *exec.Cmd, sink io.Writer) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	go func() {
		buffer := make([]byte, 4096)
		for {
			count, readError := stdout.Read(buffer)
			_, _ = sink.Write(buffer[:count])
			if readError != nil {
				return
			}
		}
	}()
	return command.Wait() // want `stdout pipe`
}
