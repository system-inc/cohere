// Package child_guard is the cure the tests configure, and the package that holds the cure is never flagged, even for
// the shape the rule flags everywhere else. Run, CombinedOutput and Capture stand in for the real ones, with their
// signatures, for the fixtures that call them.
package child_guard

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

type Options struct {
	Unbounded bool
}

func Run(command *exec.Cmd, options Options) error { return command.Run() }

func CombinedOutput(command *exec.Cmd, options Options) ([]byte, error) {
	return command.CombinedOutput()
}

func Capture(captureContext context.Context, command *exec.Cmd) (standardOutput, standardError []byte, runError error) {
	return nil, nil, command.Run()
}

func buffered() []byte {
	var output bytes.Buffer
	command := exec.Command("sh")
	command.WaitDelay = time.Second
	command.Stdout = &output
	_ = command.Run()
	return output.Bytes()
}
