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

// A terminate sent to a mixed-repository run, the way a timeout or a caller's `kill` sends it, reaches the
// project runs it started and the engines they started, and the run ends as terminated only once none of
// them is left (#nqb3mjv). Before, the run ended alone and its Swift child went on, with its engine and
// everything the engine had started.
func TestATerminatedMixedRunLeavesNoChildBehind(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	marks := test.TempDir()
	enginePid := filepath.Join(marks, "engine.pid")
	compilerPid := filepath.Join(marks, "compiler.pid")
	// An engine that starts a long compile of its own and waits on it, as the engine's types phase waits on
	// swift build.
	engine := fakeSwiftEngine(test, "echo $$ > '"+enginePid+".partial' && mv '"+enginePid+".partial' '"+enginePid+"'\n"+
		"sleep 300 &\necho $! > '"+compilerPid+".partial' && mv '"+compilerPid+".partial' '"+compilerPid+"'\nwait")
	root := newRepository(test, nil)
	writeTree(
		test,
		filepath.Join(root, "apple", "Toy"),
		map[string]string{"Package.swift": discoveryPackage, "Sources/Toy/Toy.swift": "let toy = 1\n"},
	)

	run := exec.Command(binary, "--no-fix")
	run.Dir = root
	run.Env = append(os.Environ(), "COHERE_SWIFT_ENGINE="+engine, "COHERE_VERDICT_FD=")
	if startError := run.Start(); startError != nil {
		test.Fatal(startError)
	}
	// Ignored: a run already ended, as the test means it to, is what the kill asks for.
	defer run.Process.Kill()

	readPid := func(path string) int {
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			if contents, readError := os.ReadFile(path); readError == nil {
				if pid, parseError := strconv.Atoi(strings.TrimSpace(string(contents))); parseError == nil {
					return pid
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		test.Fatalf("%s was never written: the engine did not start", filepath.Base(path))
		return 0
	}
	engineProcess, compilerProcess := readPid(enginePid), readPid(compilerPid)

	if signalError := run.Process.Signal(syscall.SIGTERM); signalError != nil {
		test.Fatal(signalError)
	}
	// Bounded well under the compile's 300s: a run that waits for its children to end on their own, rather
	// than ending them, also exits as terminated, but only once the compile finishes.
	waited := make(chan error, 1)
	go func() { waited <- run.Wait() }()
	var waitError error
	select {
	case waitError = <-waited:
	case <-time.After(time.Minute):
		test.Fatal("a minute after the terminate the run was still waiting on its children")
	}
	var exited *exec.ExitError
	if !errors.As(waitError, &exited) || exited.ExitCode() != 128+int(syscall.SIGTERM) {
		test.Errorf(
			"a terminated run ended with %v, want exit %d, the terminate's",
			waitError,
			128+int(syscall.SIGTERM),
		)
	}

	// The engine and its compiler are gone, or going: each was signaled before the run ended, so allow
	// the kernel a moment to reap them.
	for name, pid := range map[string]int{"engine": engineProcess, "compiler": compilerProcess} {
		deadline := time.Now().Add(10 * time.Second)
		for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if syscall.Kill(pid, 0) == nil {
			// Ignored: a process gone by now is what the kill asks for, and the test fails below either way.
			_ = syscall.Kill(pid, syscall.SIGKILL)
			test.Errorf("the %s (pid %d) outlived the terminated run", name, pid)
		}
	}
}
