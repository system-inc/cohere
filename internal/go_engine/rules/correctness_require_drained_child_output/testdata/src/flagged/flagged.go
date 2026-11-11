package flagged

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	process "os/exec"
)

func use(...any) {}

// Servers.app's shape: the table is read after the wait, so a table past the pipe buffer never lets ps exit.
func sampleProcesses() ([]byte, error) {
	command := exec.Command("ps", "-eo", "pid,ppid,%cpu")
	stdout, pipeError := command.StdoutPipe()
	if pipeError != nil {
		return nil, pipeError
	}
	if startError := command.Start(); startError != nil {
		return nil, startError
	}
	if waitError := command.Wait(); waitError != nil { // want `The stdout pipe is not read before .Wait. on some path here`
		return nil, waitError
	}
	return io.ReadAll(stdout)
}

// The pipe is drained on one branch only, so the other branch waits on a full pipe.
func drainWhenVerbose(command *exec.Cmd, verbose bool) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	if verbose {
		output, _ := io.ReadAll(stdout)
		use(output)
	}
	return command.Wait() // want `stdout pipe is not read before .Wait.`
}

// Run starts and waits in one call, so a pipe taken before it is never read in time.
func runWithPipe(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	runError := command.Run() // want `stdout pipe is not read before .Run.`
	use(stdout)
	return runError
}

// A stderr pipe fills the same way.
func readErrorsLate(command *exec.Cmd) ([]byte, error) {
	stderr, _ := command.StderrPipe()
	_ = command.Start()
	waitError := command.Wait() // want `The stderr pipe is not read before .Wait.`
	output, _ := io.ReadAll(stderr)
	return output, waitError
}

// A pipe never mentioned before the wait is never read: Go wants the variable used, and a blank assignment after
// the wait reads nothing.
func forgotten(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	waitError := command.Wait() // want `stdout pipe`
	_ = stdout
	return waitError
}

// A deferred Close runs on the way out, after the wait, and reads nothing.
func closedOnTheWayOut(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	defer stdout.Close()
	_ = command.Start()
	return command.Wait() // want `stdout pipe`
}

// The first turn of the loop waits before the read that comes after the wait.
func pollThenRead(command *exec.Cmd) []byte {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	var output []byte
	for {
		if waitError := command.Wait(); waitError == nil { // want `stdout pipe`
			return output
		}
		output, _ = io.ReadAll(stdout)
	}
}

// A pipe made inside a loop, waited on before it is read, on every turn.
func eachName(names []string) {
	for _, name := range names {
		command := exec.Command("ls", name)
		stdout, _ := command.StdoutPipe()
		_ = command.Start()
		_ = command.Wait() // want `stdout pipe`
		scanner := bufio.NewScanner(stdout)
		use(scanner)
	}
}

// Both pipes undrained at one wait are two findings, one per stream.
func bothPipes(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	stderr, _ := command.StderrPipe()
	_ = command.Start()
	waitError := command.Wait() // want `The stdout pipe` `The stderr pipe`
	use(stdout, stderr)
	return waitError
}

// A command reached through a field is named by its path, and an alias of os/exec is still os/exec.
type job struct{ command *process.Cmd }

func runJob(current job) error {
	stdout, _ := current.command.StdoutPipe()
	_ = current.command.Start()
	waitError := current.command.Wait() // want `stdout pipe`
	use(stdout)
	return waitError
}

// A var declaration makes a pipe as much as := does, and a value exec.Cmd is named like a pointer.
func valueCommand() error {
	var command exec.Cmd
	var stdout, _ = command.StdoutPipe()
	_ = command.Start()
	waitError := command.Wait() // want `stdout pipe`
	_, _ = io.Copy(os.Stdout, stdout)
	return waitError
}

// A wait in a function literal is judged on the literal's graph, where the pipe it makes is never read.
var inLiteral = func(command *exec.Cmd) error {
	stdout, _ := command.StdoutPipe()
	_ = command.Start()
	waitError := command.Wait() // want `stdout pipe`
	use(stdout)
	return waitError
}
