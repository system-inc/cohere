//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A command started in its own group and stopped at the deadline takes its children with it: a shell that started a
// sleep in the background, as the engine starts go list, is stopped, and so is the sleep, which a kill of the
// shell's pid alone would have left running.
func TestStopGroupStopsTheChildrenToo(test *testing.T) {
	test.Parallel()
	childFile := filepath.Join(test.TempDir(), "child")
	command := exec.Command("sh", "-c", `sleep 60 & echo $! > "$0"; wait`, childFile)
	command.SysProcAttr = ownProcessGroup()
	if startError := command.Start(); startError != nil {
		test.Fatal(startError)
	}
	child := 0
	for deadline := time.Now().Add(10 * time.Second); child == 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			// Ignored: the test has already failed, and this only keeps the shell from outliving it.
			_ = stopGroup(command)
			test.Fatal("the shell never said its child's pid")
		}
		contents, readError := os.ReadFile(childFile)
		if readError == nil && strings.HasSuffix(string(contents), "\n") {
			parsed, parseError := strconv.Atoi(strings.TrimSpace(string(contents)))
			if parseError != nil {
				test.Fatal(parseError)
			}
			child = parsed
		}
	}

	if stopError := stopGroup(command); stopError != nil {
		test.Fatal(stopError)
	}
	// Ignored: the shell was killed, so Wait reports the signal, which is what was asked for.
	_ = command.Wait()
	if status, isWaitStatus := command.ProcessState.Sys().(syscall.WaitStatus); !isWaitStatus ||
		status.Signal() != syscall.SIGKILL {
		test.Errorf("the shell ended %v, not by SIGKILL", command.ProcessState)
	}
	// The sleep, orphaned once the shell is gone, is reaped by init; until then a signal 0 still finds it, so its end
	// is waited for rather than read once.
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if signalError := syscall.Kill(child, 0); errors.Is(signalError, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			// Ignored: the test has failed, and this only keeps the sleep from outliving it.
			_ = syscall.Kill(child, syscall.SIGKILL)
			test.Fatalf("the shell's child %d still runs after its group was stopped", child)
		}
	}
}
