//go:build unix

// The stand-in engines here are /bin/sh scripts, and the process-group behavior they prove exists only
// on Unix, so the file builds there.

package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// fakeSwiftEngine writes a shell script standing in for cohere-swift, so the process boundary can be driven
// through every way an engine ends without building the real one.
func fakeSwiftEngine(test *testing.T, body string) string {
	test.Helper()
	path := filepath.Join(test.TempDir(), "cohere-swift")
	if writeError := executable_testing.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	return path
}

func runFakeSwiftEngine(test *testing.T, body string, mode engineMode) (string, string, int, error) {
	test.Helper()
	var out bytes.Buffer
	var standardError bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newEngineRun(stream, mode, swiftLanguage, "", time.Now())
	exitCode, runEngineBinaryError := runEngineBinary(
		fakeSwiftEngine(test, body),
		[]string{"--contract", "1"},
		run,
		&standardError,
	)
	flushStream(test, stream)
	return out.String(), standardError.String(), exitCode, runEngineBinaryError
}

// Not parallel: its subtests drive check runs through engineRun, whose record acceptors set the package-level
// activeSummary
func TestRunEngineBinaryEndings(test *testing.T) {
	fixtures, absolutePathError := filepath.Abs(filepath.Join("..", "..", "swift", "Contract"))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}

	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("a real stream streams and exits with its summary", func(test *testing.T) {
		out, _, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(
			test,
			"cat '"+filepath.Join(fixtures, "Findings.jsonl")+"'; exit 1",
			engineModeCheck,
		)
		if runFakeSwiftEngineError != nil || exitCode != 1 {
			test.Fatalf("exit %d, err %v\n%s", exitCode, runFakeSwiftEngineError, out)
		}
		if !strings.Contains(out, "[cohere-swift/force-unwrapping/forceUnwrap]") || !strings.Contains(out, "phases: ") {
			test.Errorf("the stream was not rendered:\n%s", out)
		}
	})

	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("the control: a clean stream exits 0", func(test *testing.T) {
		_, _, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(
			test,
			"cat '"+filepath.Join(fixtures, "Clean.jsonl")+"'",
			engineModeCheck,
		)
		if runFakeSwiftEngineError != nil || exitCode != 0 {
			test.Fatalf("a clean engine run exited %d, err %v", exitCode, runFakeSwiftEngineError)
		}
	})

	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("exit 2 passes stderr through and says nothing was checked", func(test *testing.T) {
		_, standardError, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(
			test,
			"echo 'cohere-swift: --timing is not implemented for Swift yet' >&2; exit 2",
			engineModeCheck,
		)
		if runFakeSwiftEngineError == nil ||
			exitCode != 1 ||
			!strings.Contains(runFakeSwiftEngineError.Error(), "did not run, so nothing was checked") {
			test.Fatalf("exit %d, err %v", exitCode, runFakeSwiftEngineError)
		}
		if !strings.Contains(standardError, "--timing is not implemented") {
			test.Errorf("the engine's own sentence was not passed through: %q", standardError)
		}
	})

	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("a signal is named as a signal, never as an exit code", func(test *testing.T) {
		out, _, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(
			test,
			"head -2 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; kill -9 $$",
			engineModeCheck,
		)
		if runFakeSwiftEngineError == nil || exitCode != 1 {
			test.Fatalf("a killed engine exited %d, err %v", exitCode, runFakeSwiftEngineError)
		}
		if !strings.Contains(runFakeSwiftEngineError.Error(), "was killed by") {
			test.Errorf("the signal was not named: %v", runFakeSwiftEngineError)
		}
		if strings.Contains(runFakeSwiftEngineError.Error(), "exited -1") {
			test.Errorf("printed an exit code the process never chose: %v", runFakeSwiftEngineError)
		}
		if !strings.Contains(out, "did not run (the Swift engine was killed") {
			test.Errorf("the phase line did not say what was not reached:\n%s", out)
		}
	})

	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("a broken record kills the engine rather than waiting on it", func(test *testing.T) {
		// The engine sleeps past any run, so a front door that waited on it hangs, a hang go test's -timeout fails.
		_, _, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(
			test,
			"head -1 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; echo 'Compiling swift-syntax'; sleep 100000",
			engineModeCheck,
		)
		if runFakeSwiftEngineError == nil ||
			exitCode != 1 ||
			!strings.Contains(runFakeSwiftEngineError.Error(), "the Swift engine is broken") {
			test.Fatalf("exit %d, err %v", exitCode, runFakeSwiftEngineError)
		}
	})

	// The child is started before the broken record, so it is always alive when the engine is killed.
	// Killing only the engine left it holding the pipes, and the front door waited on it. It sleeps past any
	// run, so a front door that waits on it again hangs, a hang go test's -timeout fails.
	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("a broken engine's children die with it", func(test *testing.T) {
		childFile := filepath.Join(test.TempDir(), "child")
		killChildAtCleanup(test, childFile)
		_, _, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(test,
			"sleep 100000 & echo $! > '"+
				childFile+
				"'; head -1 '"+
				filepath.Join(fixtures, "Clean.jsonl")+
				"'; echo 'Compiling swift-syntax'; wait",
			engineModeCheck)
		if runFakeSwiftEngineError == nil ||
			exitCode != 1 ||
			!strings.Contains(runFakeSwiftEngineError.Error(), "the Swift engine is broken") {
			test.Fatalf("exit %d, err %v", exitCode, runFakeSwiftEngineError)
		}
		requireProcessEnded(test, childFile)
	})

	// `SIGTERM` rather than an interrupt, because a POSIX shell starts a background job with interrupts
	// ignored, and the stand-in's child must be one the signal can end. The engine waits on a child that sleeps
	// past any run, so a signal not passed on leaves the run waiting, a hang go test's -timeout fails.
	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("a signal cohere receives is passed to the engine and everything it started", func(test *testing.T) {
		childFile := filepath.Join(test.TempDir(), "child")
		killChildAtCleanup(test, childFile)
		interrupts := make(chan os.Signal, 1)
		var out bytes.Buffer
		var standardError bytes.Buffer
		stream := bufio.NewWriter(&out)
		run := newEngineRun(stream, engineModeCheck, swiftLanguage, "", time.Now())
		go func() {
			waitForFile(test, childFile)
			interrupts <- syscall.SIGTERM
		}()
		exitCode, runEngineBinaryUntilError := runEngineBinaryUntil(
			fakeSwiftEngine(
				test,
				"sleep 100000 & echo $! > '"+childFile+"'; head -2 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; wait",
			),
			[]string{"--contract", "1"}, run, &standardError, interrupts)
		flushStream(test, stream)
		if runEngineBinaryUntilError == nil ||
			exitCode != 1 ||
			!strings.Contains(runEngineBinaryUntilError.Error(), "was killed by") {
			test.Fatalf(
				"an engine signalled through cohere exited %d, err %v\n%s",
				exitCode,
				runEngineBinaryUntilError,
				out.String(),
			)
		}
		requireProcessEnded(test, childFile)
	})
}

// waitForFile waits for a stand-in engine to write a file, failing the test after ten seconds.
func waitForFile(test *testing.T, path string) {
	test.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if contents, readError := os.ReadFile(path); readError == nil && len(contents) > 0 {
			return
		}
	}
	test.Errorf("the stand-in engine never wrote %s", path)
}

// requireProcessEnded fails unless the process whose id a stand-in engine wrote to path is gone. An
// orphan is reaped by launchd or init a moment after it dies, so it is given two seconds.
func requireProcessEnded(test *testing.T, path string) {
	test.Helper()
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatalf("the stand-in engine never wrote its child's id: %v", readError)
	}
	child, parseError := strconv.Atoi(strings.TrimSpace(string(contents)))
	if parseError != nil {
		test.Fatalf("the child id %q is not a number: %v", contents, parseError)
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(child, 0) == syscall.ESRCH {
			return
		}
	}
	// Ignored: a child gone by now is what the kill asks for, and the test fails below either way.
	_ = syscall.Kill(child, syscall.SIGKILL)
	test.Errorf("the engine's child %d outlived it", child)
}

// standInChild is the command a stand-in engine starts as its child, the one killChildAtCleanup may end.
const standInChild = "sleep 100000"

// killChildAtCleanup ends, when the test does, the stand-in engine's child whose id the engine wrote to path, if
// it is still running. requireProcessEnded kills it too, but only a test that gets that far: one that fails
// before it, or hangs until go test's -timeout, would leave the sleep on the machine for a day.
//
// It kills by id, never by name, and only a process still running standInChild, since an id is reused once its
// process ends and a cleanup that killed whatever holds it now could end someone else's work.
func killChildAtCleanup(test *testing.T, path string) {
	test.Helper()
	test.Cleanup(func() {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			// The engine never wrote the id, so it never started the child, and there is nothing to end.
			return
		}
		child, parseError := strconv.Atoi(strings.TrimSpace(string(contents)))
		if parseError != nil {
			return
		}
		command, psError := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(child)).Output()
		if psError != nil || strings.TrimSpace(string(command)) != standInChild {
			// ps fails on an id no process holds, and a different command means the id was reused.
			return
		}
		// Ignored: the child ending on its own between the look and the kill is the outcome the kill asks for.
		_ = syscall.Kill(child, syscall.SIGKILL)
	})
}

// TestAStandInEnginesChildIsKilledWhenItsTestEnds: a child a test leaves running is gone once the test's
// cleanups have run, so a stand-in engine test that fails early or hangs leaves no sleep behind.
func TestAStandInEnginesChildIsKilledWhenItsTestEnds(test *testing.T) {
	test.Parallel()
	sleeper := exec.Command("sleep", "100000")
	if startError := sleeper.Start(); startError != nil {
		test.Fatal(startError)
	}
	childFile := filepath.Join(test.TempDir(), "child")
	if writeError := os.WriteFile(childFile, []byte(strconv.Itoa(sleeper.Process.Pid)+"\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	ended := make(chan struct{})
	go func() {
		// Ignored: the sleep ends by the kill, so its wait reports that signal; what matters is that it ends.
		_ = sleeper.Wait()
		close(ended)
	}()
	// The subtest stands for a stand-in engine test that leaves its child running; its cleanups run as it ends.
	// Not parallel: the wait below needs its cleanup to have killed the child, and a parallel subtest would not
	// start until this test returned
	test.Run("a test that leaves its child running", func(test *testing.T) {
		killChildAtCleanup(test, childFile)
	})
	// No deadline: a cleanup that did not kill the child leaves this waiting on a sleep that outlasts any run, a
	// hang go test's -timeout fails.
	<-ended
}
