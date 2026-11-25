package allowed

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
)

// A WaitGroup joins the reader before the child is waited on.
func joinedByGroup(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		_, _ = io.Copy(os.Stdout, stdout)
	}()
	group.Wait()
	return command.Wait()
}

// A channel the reader closes is received before the wait.
func joinedByChannel(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(os.Stdout, stdout)
	}()
	<-done
	return command.Wait()
}

// The reader sends its result, and the result is received before the wait.
func joinedByResult(command *exec.Cmd) ([]byte, error) {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	results := make(chan []byte, 1)
	go func() {
		output, _ := io.ReadAll(stdout)
		results <- output
	}()
	output := <-results
	return output, command.Wait()
}

// A select on the reader's channel joins it as a receive does.
func joinedBySelect(command *exec.Cmd, stop chan struct{}) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(os.Stdout, stdout)
	}()
	select {
	case <-done:
	case <-stop:
		return nil
	}
	return command.Wait()
}

// A WaitGroup's Go, waited on before the child.
func groupGoJoined(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	var group sync.WaitGroup
	group.Go(func() {
		_, _ = io.Copy(io.Discard, stdout)
	})
	group.Wait()
	return command.Wait()
}

// The signal is handed to a helper, which may join the reader where the rule cannot see.
func signalHandedOn(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(os.Stdout, stdout)
	}()
	awaitAll(done)
	return command.Wait()
}

func awaitAll(channels ...chan struct{}) {
	for _, channel := range channels {
		<-channel
	}
}

// Another goroutine forwards the join, so the rule can't follow it.
func joinForwarded(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(os.Stdout, stdout)
	}()
	go func() {
		<-done
		close(finished)
	}()
	<-finished
	return command.Wait()
}

// A reader that calls a function of this package may signal through it.
func callsThisPackage(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	go func() {
		_, _ = io.Copy(os.Stdout, stdout)
		finish()
	}()
	awaitFinish()
	return command.Wait()
}

var finished = make(chan struct{})

func finish()      { close(finished) }
func awaitFinish() { <-finished }

// A reader that holds a mutex or a context synchronizes in a way the rule can't read.
func withMutex(command *exec.Cmd, lock *sync.Mutex, cancel context.CancelFunc) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	go func() {
		lock.Lock()
		defer lock.Unlock()
		_, _ = io.Copy(os.Stdout, stdout)
		cancel()
	}()
	lock.Lock()
	defer lock.Unlock()
	return command.Wait()
}

// A channel reached through a field joins the reader by a value the rule doesn't name.
type job struct{ done chan struct{} }

func throughAField(command *exec.Cmd, current job) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	go func() {
		defer close(current.done)
		_, _ = io.Copy(os.Stdout, stdout)
	}()
	<-current.done
	return command.Wait()
}

// The function reads the pipe itself, so no goroutine races the wait.
func readInline(command *exec.Cmd) ([]byte, error) {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	output, _ := io.ReadAll(stdout)
	return output, command.Wait()
}

// A goroutine that doesn't touch the pipe isn't its reader.
func unrelatedGoroutine(command *exec.Cmd) ([]byte, error) {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	go func() { _, _ = os.Stderr.WriteString("started\n") }()
	output, _ := io.ReadAll(stdout)
	return output, command.Wait()
}

// The pipe belongs to one command and the wait to another.
func otherCommand(first *exec.Cmd, second *exec.Cmd) error {
	stdout, _ := first.StdoutPipe()
	_ = first.Start()
	go io.Copy(os.Stdout, stdout)
	return second.Wait()
}

// No wait on any path: the zombie companion's case.
func neverWaited(command *exec.Cmd) {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	go io.Copy(os.Stdout, stdout)
}

// A goroutine started before the reader forwards its signal, so the receive on the path names another channel.
func joinForwardedFromBefore(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		<-done
		close(finished)
	}()
	go func() {
		defer close(done)
		_, _ = io.Copy(os.Stdout, stdout)
	}()
	<-finished
	return command.Wait()
}

// A condition joins the reader, which the rule can't read, so it stays quiet.
func joinedByCondition(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	condition := sync.NewCond(&sync.Mutex{})
	copied := false
	go func() {
		_, _ = io.Copy(os.Stdout, stdout)
		condition.L.Lock()
		copied = true
		condition.Broadcast()
		condition.L.Unlock()
	}()
	condition.L.Lock()
	for !copied {
		condition.Wait()
	}
	condition.L.Unlock()
	return command.Wait()
}

// An atomic flag joins the reader, a synchronization the rule can't read, so it stays quiet.
func joinedByAtomicFlag(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	var copied atomic.Bool
	go func() {
		_, _ = io.Copy(os.Stdout, stdout)
		copied.Store(true)
	}()
	for !copied.Load() {
		runtime.Gosched()
	}
	return command.Wait()
}
