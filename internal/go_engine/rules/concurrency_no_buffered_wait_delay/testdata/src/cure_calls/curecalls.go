// Package cure_calls hands commands to the configured cure package and to a lookalike of it.
package cure_calls

import (
	"context"
	"os"
	"os/exec"
	"time"

	"cure/child_guard"
	elsewhere "lookalike/child_guard"
)

// child_guard.Run wraps every output in a writer of its own, so exec copies through pipes whatever the outputs are.
func handedToRun() {
	command := exec.Command("sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	command.Stdout = os.Stdout
	_ = child_guard.Run(command, child_guard.Options{})
}

// child_guard.CombinedOutput gives the command a bytes.Buffer.
func handedToCombinedOutput(commandContext context.Context) ([]byte, error) {
	command := exec.CommandContext(commandContext, "sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	return child_guard.CombinedOutput(command, child_guard.Options{Unbounded: true})
}

func literalHandedToRun() {
	_ = child_guard.Run(&exec.Cmd{Path: "/bin/true", WaitDelay: time.Second}, child_guard.Options{}) // want `^This command sets WaitDelay`
}

// child_guard.Capture gives the command *os.File pipes, so exec copies nothing and WaitDelay only bounds the wait.
func handedToCapture(commandContext context.Context) ([]byte, []byte, error) {
	command := exec.CommandContext(commandContext, "sh")
	command.WaitDelay = time.Second
	return child_guard.Capture(commandContext, command)
}

// The rule knows the cure by its package path, not by the name a call is spelled with.
func handedToALookalike() {
	command := exec.Command("sh")
	command.WaitDelay = time.Second
	command.Stdout = os.Stdout
	_ = elsewhere.Run(command, elsewhere.Options{})
	_, _ = elsewhere.CombinedOutput(command, elsewhere.Options{})
}

// A command without WaitDelay handed to the cure is fine.
func withoutWaitDelay() {
	_ = child_guard.Run(exec.Command("sh"), child_guard.Options{})
}
