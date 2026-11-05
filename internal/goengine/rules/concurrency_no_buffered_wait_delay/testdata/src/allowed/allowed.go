// Package allowed holds commands the rule must leave alone: each WaitDelay goes only to files or nowhere, or carries
// its reason.
package allowed

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"
)

func fileOutputs() {
	command := exec.Command("sh")
	command.WaitDelay = time.Second
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	_ = command.Run()
}

func createdFile(path string) error {
	file, createError := os.Create(path)
	if createError != nil {
		return createError
	}
	defer file.Close()
	command := exec.Command("sh")
	command.WaitDelay = time.Second
	command.Stdout, command.Stderr = file, nil
	return command.Run()
}

func nilOutputs() {
	command := exec.Command("sh")
	command.WaitDelay = time.Second
	command.Stdout = nil
	_ = command.Run()
}

func fileLiteral() {
	_ = (&exec.Cmd{Path: "/bin/true", Stdout: os.Stdout, Stderr: nil, WaitDelay: time.Second}).Run()
}

func markerAbove() {
	var output bytes.Buffer
	command := exec.Command("sh")
	// WaitDelay: the child never starts a descendant, and a late drain here is a hang we want reported.
	command.WaitDelay = time.Second
	command.Stdout = &output
	_ = command.Run()
}

func markerWrapped() {
	command := exec.Command("sh")
	// WaitDelay: a reason long enough to wrap
	// onto a second line above the code it explains.
	command.WaitDelay = time.Second
	_, _ = command.CombinedOutput()
}

func markerOnTheLine() *exec.Cmd {
	command := exec.Command("sh")
	command.WaitDelay = time.Second // WaitDelay: every caller hands it *os.File outputs.
	return command
}

func markerOnALiteral() *exec.Cmd {
	return &exec.Cmd{
		Path: "/bin/true",
		// WaitDelay: the reason, on the field.
		WaitDelay: time.Second,
	}
}

func noWaitDelay(commandContext context.Context) ([]byte, error) {
	var output bytes.Buffer
	command := exec.CommandContext(commandContext, "sh")
	command.Stdout = &output
	if runError := command.Run(); runError != nil {
		return nil, runError
	}
	return exec.Command("sh").CombinedOutput()
}

func helperWithoutWaitDelay() *exec.Cmd {
	return exec.Command("sh")
}

// Two commands: the one with WaitDelay writes to files, the buffered one has none.
func anotherCommandBuffered() {
	var output bytes.Buffer
	quiet := exec.Command("sh")
	quiet.WaitDelay = time.Second
	quiet.Stdout = os.Stdout
	loud := exec.Command("sh")
	loud.Stdout = &output
	_, _ = exec.Command("sh").Output()
	_ = quiet.Run()
	_ = loud.Run()
}
