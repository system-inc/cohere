// Package unconfigured holds shapes the configured rule flags, with no want: run with the cure option empty, the rule
// is off and must report none of them.
package unconfigured

import (
	"bytes"
	"os/exec"
	"time"

	"cure/child_guard"
)

func fieldWithBuffer() {
	var output bytes.Buffer
	command := exec.Command("sh")
	command.WaitDelay = time.Second
	command.Stdout = &output
	_ = command.Run()
}

func returnedLiteral() *exec.Cmd {
	return &exec.Cmd{Path: "/bin/true", WaitDelay: time.Second}
}

func handedToRun() {
	command := exec.Command("sh")
	command.WaitDelay = time.Second
	_ = child_guard.Run(command, child_guard.Options{})
}

func markerWithoutAReason() {
	command := exec.Command("sh")
	// WaitDelay:
	command.WaitDelay = time.Second
	_, _ = command.Output()
}
