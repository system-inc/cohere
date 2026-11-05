// Package flagged holds commands that set WaitDelay while their output is buffered, here or by a caller, each of
// which the rule must report.
package flagged

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"time"
)

func fieldWithBuffer() {
	var output bytes.Buffer
	command := exec.Command("sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory, .* Run it with Capture from .cure/childguard., which reads each stream to its end with no timer, or, if the delay is meant, say why in a .// WaitDelay: <reason>. comment at the end of its line or directly above it\.$`
	command.Stdout = &output
	_ = command.Run()
}

func stderrToBuilderBeforeDelay() {
	var errors strings.Builder
	command := exec.Command("sh")
	command.Stderr = &errors
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	_ = command.Run()
}

func bothStreamsAtOnce() {
	var output bytes.Buffer
	command := exec.Command("sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	command.Stdout, command.Stderr = &output, &output
	_ = command.Run()
}

// An io.Writer may hold an *os.File, but the rule reads the type the code wrote, and exec copies anything else.
func writerOfUnknownKind(writer io.Writer) {
	command := exec.Command("sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	command.Stdout = writer
	_ = command.Run()
}

func compositeLiteral() {
	var output bytes.Buffer
	_ = (&exec.Cmd{Path: "/bin/true", Stdout: &output, WaitDelay: time.Second}).Run() // want `^This command sets WaitDelay`
}

func compositeLiteralBufferedLater() {
	var output bytes.Buffer
	command := &exec.Cmd{Path: "/bin/true", WaitDelay: time.Second} // want `^This command sets WaitDelay`
	command.Stderr = &output
	_ = command.Run()
}

func compositeLiteralDeclared() {
	var command = exec.Cmd{Path: "/bin/true", WaitDelay: time.Second} // want `^This command sets WaitDelay`
	_, _ = command.Output()
}

func combinedOutput() {
	command := exec.Command("sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	_, _ = command.CombinedOutput()
}

func output() {
	command := exec.Command("sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	_, _ = command.Output()
}

// Adamic's markdownblocks shape: the helper sets WaitDelay and returns the command, and its caller buffers it.
func returnedHelper(commandContext context.Context, name string) *exec.Cmd {
	command := exec.CommandContext(commandContext, name)
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	return command
}

func callerBuffersHelper(commandContext context.Context) {
	command := returnedHelper(commandContext, "sh")
	var standardOutput, standardError bytes.Buffer
	command.Stdout = &standardOutput
	command.Stderr = &standardError
	_ = command.Run()
}

func returnedWithError() (*exec.Cmd, error) {
	command := exec.Command("sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	return command, nil
}

func returnedLiteral() *exec.Cmd {
	return &exec.Cmd{Path: "/bin/true", WaitDelay: time.Second} // want `^This command sets WaitDelay`
}

func returnedPointerToValue() *exec.Cmd {
	var command exec.Cmd
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	return &command
}

type runner struct {
	*exec.Cmd
}

func embedded() {
	var output bytes.Buffer
	run := runner{exec.Command("sh")}
	run.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	run.Stdout = &output
	_ = run.Run()
}

type job struct {
	command *exec.Cmd
}

func throughAField(work *job) {
	var output bytes.Buffer
	work.command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	work.command.Stdout = &output
	_ = work.command.Run()
}

func bufferedInAClosure() {
	command := exec.Command("sh")
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	run := func() ([]byte, error) { return command.CombinedOutput() }
	_, _ = run()
}

func markerWithoutAReason() {
	command := exec.Command("sh")
	// WaitDelay:
	command.WaitDelay = time.Second // want `^This WaitDelay on a buffered command is marked and gives no reason\. .* reach for Capture from .cure/childguard. instead`
	_, _ = command.Output()
}

func markerTooFarAbove() {
	command := exec.Command("sh")
	// WaitDelay: this reason is for the line just below, not for the assignment after it.
	command.Env = nil
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	_, _ = command.Output()
}

// A sentence that mentions the marker is not one: the marker opens the comment.
func markerOnlyMentioned() {
	command := exec.Command("sh")
	// This used to carry a WaitDelay: reason, and lost it.
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	_, _ = command.Output()
}

// Adamic reads only line comments for the marker, so a block comment excuses nothing.
func markerInABlockComment() {
	command := exec.Command("sh")
	/* WaitDelay: a reason in a block comment. */
	command.WaitDelay = time.Second // want `^This command sets WaitDelay while its output goes to memory`
	_, _ = command.Output()
}
