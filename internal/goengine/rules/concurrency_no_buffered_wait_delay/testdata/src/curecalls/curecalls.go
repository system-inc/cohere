// Package curecalls hands commands to the configured cure package and to a lookalike of it.
package curecalls

import (
	"context"
	"os"
	"os/exec"
	"time"

	"cure/childguard"
	elsewhere "lookalike/childguard"
)

// childguard.Run wraps every output in a writer of its own, so exec copies through pipes whatever the outputs are.
func handedToRun() {
	command := exec.Command("sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	command.Stdout = os.Stdout
	_ = childguard.Run(command, childguard.Options{})
}

// childguard.CombinedOutput gives the command a bytes.Buffer.
func handedToCombinedOutput(commandContext context.Context) ([]byte, error) {
	command := exec.CommandContext(commandContext, "sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	return childguard.CombinedOutput(command, childguard.Options{Unbounded: true})
}

func literalHandedToRun() {
	_ = childguard.Run(&exec.Cmd{Path: "/bin/true", WaitDelay: time.Second}, childguard.Options{}) // want `^This command sets WaitDelay`
}

// childguard.Capture gives the command *os.File pipes, so exec copies nothing and WaitDelay only bounds the wait.
func handedToCapture(commandContext context.Context) ([]byte, []byte, error) {
	command := exec.CommandContext(commandContext, "sh")
	command.WaitDelay = time.Second
	return childguard.Capture(commandContext, command)
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
	_ = childguard.Run(exec.Command("sh"), childguard.Options{})
}
