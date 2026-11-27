//go:build unix

// The stand-in children here are /bin/sh scripts, and the process-group behavior they prove exists only
// on Unix, so the file builds there.

package main

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
	"github.com/system-inc/cohere/internal/testing/process_reading"
)

// fakeSwiftBinary writes a shell script standing in for cohere-swift, so the process boundary can be driven
// through every way a child ends without building the real one.
func fakeSwiftBinary(test *testing.T, body string) string {
	test.Helper()
	path := filepath.Join(test.TempDir(), "cohere-swift")
	if writeError := executable_testing.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	return path
}

func runFakeSwiftBinary(test *testing.T, body string, mode contractMode) (string, string, int, error) {
	test.Helper()
	var out bytes.Buffer
	var standardError bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newContractRun(stream, mode, swiftLanguage, "", time.Now())
	exitCode, runContractBinaryError := runContractBinary(
		fakeSwiftBinary(test, body),
		[]string{"--contract", "1"},
		run,
		&standardError,
	)
	flushStream(test, stream)
	return out.String(), standardError.String(), exitCode, runContractBinaryError
}

// Not parallel: its subtests drive check runs through contractRun, whose record acceptors set the package-level
// activeSummary
func TestRunContractBinaryEndings(test *testing.T) {
	fixtures, absolutePathError := filepath.Abs(contractFixtureDirectory)
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}

	// Not parallel: it drives a check run through contractRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("a real stream streams and exits with its summary", func(test *testing.T) {
		out, _, exitCode, runFakeSwiftBinaryError := runFakeSwiftBinary(
			test,
			"cat '"+filepath.Join(fixtures, "Findings.jsonl")+"'; exit 1",
			contractModeCheck,
		)
		if runFakeSwiftBinaryError != nil || exitCode != 1 {
			test.Fatalf("exit %d, err %v\n%s", exitCode, runFakeSwiftBinaryError, out)
		}
		if !strings.Contains(out, "[cohere-swift/force-unwrapping/forceUnwrap]") || !strings.Contains(out, "phases: ") {
			test.Errorf("the stream was not rendered:\n%s", out)
		}
	})

	// Not parallel: it drives a check run through contractRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("the control: a clean stream exits 0", func(test *testing.T) {
		_, _, exitCode, runFakeSwiftBinaryError := runFakeSwiftBinary(
			test,
			"cat '"+filepath.Join(fixtures, "Clean.jsonl")+"'",
			contractModeCheck,
		)
		if runFakeSwiftBinaryError != nil || exitCode != 0 {
			test.Fatalf("a clean child run exited %d, err %v", exitCode, runFakeSwiftBinaryError)
		}
	})

	// Not parallel: it drives a check run through contractRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("exit 2 passes stderr through and says nothing was checked", func(test *testing.T) {
		_, standardError, exitCode, runFakeSwiftBinaryError := runFakeSwiftBinary(
			test,
			"echo 'cohere-swift: --timing is not implemented for Swift yet' >&2; exit 2",
			contractModeCheck,
		)
		if runFakeSwiftBinaryError == nil ||
			exitCode != 1 ||
			!strings.Contains(runFakeSwiftBinaryError.Error(), "did not run, so nothing was checked") {
			test.Fatalf("exit %d, err %v", exitCode, runFakeSwiftBinaryError)
		}
		if !strings.Contains(standardError, "--timing is not implemented") {
			test.Errorf("the child's own sentence was not passed through: %q", standardError)
		}
	})

	// Not parallel: it drives a check run through contractRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("a signal is named as a signal, never as an exit code", func(test *testing.T) {
		out, _, exitCode, runFakeSwiftBinaryError := runFakeSwiftBinary(
			test,
			"head -2 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; kill -9 $$",
			contractModeCheck,
		)
		if runFakeSwiftBinaryError == nil || exitCode != 1 {
			test.Fatalf("a killed child exited %d, err %v", exitCode, runFakeSwiftBinaryError)
		}
		if !strings.Contains(runFakeSwiftBinaryError.Error(), "was killed by") {
			test.Errorf("the signal was not named: %v", runFakeSwiftBinaryError)
		}
		if strings.Contains(runFakeSwiftBinaryError.Error(), "exited -1") {
			test.Errorf("printed an exit code the process never chose: %v", runFakeSwiftBinaryError)
		}
		if !strings.Contains(out, "did not run (cohere-swift was killed") {
			test.Errorf("the phase line did not say what was not reached:\n%s", out)
		}
	})

	// Not parallel: it drives a check run through contractRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("a broken record kills the child rather than waiting on it", func(test *testing.T) {
		// The child sleeps past any run, so a front door that waited on it hangs, a hang go test's -timeout fails.
		_, _, exitCode, runFakeSwiftBinaryError := runFakeSwiftBinary(
			test,
			"head -1 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; echo 'Compiling swift-syntax'; sleep 100000",
			contractModeCheck,
		)
		if runFakeSwiftBinaryError == nil ||
			exitCode != 1 ||
			!strings.Contains(runFakeSwiftBinaryError.Error(), "cohere-swift is broken") {
			test.Fatalf("exit %d, err %v", exitCode, runFakeSwiftBinaryError)
		}
	})

	// The child is started before the broken record, so it is always alive when the child is killed.
	// Killing only the child left it holding the pipes, and the front door waited on it. It sleeps past any
	// run, so a front door that waits on it again hangs, a hang go test's -timeout fails.
	// Not parallel: it drives a check run through contractRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("a broken child's children die with it", func(test *testing.T) {
		childFile := filepath.Join(test.TempDir(), "child")
		killChildAtCleanup(test, childFile)
		_, _, exitCode, runFakeSwiftBinaryError := runFakeSwiftBinary(test,
			"sleep 100000 & echo $! > '"+
				childFile+
				"'; head -1 '"+
				filepath.Join(fixtures, "Clean.jsonl")+
				"'; echo 'Compiling swift-syntax'; wait",
			contractModeCheck)
		if runFakeSwiftBinaryError == nil ||
			exitCode != 1 ||
			!strings.Contains(runFakeSwiftBinaryError.Error(), "cohere-swift is broken") {
			test.Fatalf("exit %d, err %v", exitCode, runFakeSwiftBinaryError)
		}
		requireProcessEnded(test, childFile)
	})

	// `SIGTERM` rather than an interrupt, because a POSIX shell starts a background job with interrupts
	// ignored, and the stand-in's child must be one the signal can end. The child waits on a child that sleeps
	// past any run, so a signal not passed on leaves the run waiting, a hang go test's -timeout fails.
	// Not parallel: it drives a check run through contractRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("a signal cohere receives is passed to the child and everything it started", func(test *testing.T) {
		childFile := filepath.Join(test.TempDir(), "child")
		killChildAtCleanup(test, childFile)
		interrupts := make(chan os.Signal, 1)
		var out bytes.Buffer
		var standardError bytes.Buffer
		stream := bufio.NewWriter(&out)
		run := newContractRun(stream, contractModeCheck, swiftLanguage, "", time.Now())
		go func() {
			waitForFile(test, childFile)
			interrupts <- syscall.SIGTERM
		}()
		exitCode, runContractBinaryUntilError := runContractBinaryUntil(
			fakeSwiftBinary(
				test,
				"sleep 100000 & echo $! > '"+childFile+"'; head -2 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; wait",
			),
			[]string{"--contract", "1"}, run, &standardError, interrupts)
		flushStream(test, stream)
		if runContractBinaryUntilError == nil ||
			exitCode != 1 ||
			!strings.Contains(runContractBinaryUntilError.Error(), "was killed by") {
			test.Fatalf(
				"a child signalled through cohere exited %d, err %v\n%s",
				exitCode,
				runContractBinaryUntilError,
				out.String(),
			)
		}
		requireProcessEnded(test, childFile)
	})
}

// waitForFile waits for a stand-in cohere-swift to write a file, failing the test after ten seconds.
func waitForFile(test *testing.T, path string) {
	test.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if contents, readError := os.ReadFile(path); readError == nil && len(contents) > 0 {
			return
		}
	}
	test.Errorf("the stand-in cohere-swift never wrote %s", path)
}

// requireProcessEnded fails unless the process whose id a stand-in cohere-swift wrote to path is gone. An
// orphan is reaped by launchd or init a moment after it dies, so it is given two seconds.
func requireProcessEnded(test *testing.T, path string) {
	test.Helper()
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatalf("the stand-in cohere-swift never wrote its child's id: %v", readError)
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
	test.Errorf("the child's child %d outlived it", child)
}

// standInChild is the command a stand-in cohere-swift starts as its child, the one killChildAtCleanup may end.
const standInChild = "sleep 100000"

// killChildAtCleanup ends, when the test does, the stand-in cohere-swift's child whose id the child wrote to path, if
// it is still running. requireProcessEnded kills it too, but only a test that gets that far: one that fails
// before it, or hangs until go test's -timeout, would leave the sleep on the machine for a day.
//
// It kills by id, never by name, and only a process still running standInChild, since an id is reused once its
// process ends and a cleanup that killed whatever holds it now could end someone else's work. A command line that
// can't be read fails the test, since the child it couldn't look at is still running.
func killChildAtCleanup(test *testing.T, path string) {
	test.Helper()
	test.Cleanup(func() {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			// The child never wrote the id, so it never started the child, and there is nothing to end.
			return
		}
		child, parseError := strconv.Atoi(strings.TrimSpace(string(contents)))
		if parseError != nil {
			return
		}
		// Read in this process, never through ps, which is setuid on macOS and refused in a land job's sandbox.
		command, commandError := process_reading.Command(child)
		if errors.Is(commandError, process_reading.ErrNoProcess) {
			// No process holds the id, so the child has ended and there is nothing to end.
			return
		}
		if commandError != nil {
			// A command line that can't be read says nothing of the child, which is left running, so the test says
			// why.
			test.Errorf("the command of the child %d couldn't be read, so it was left running: %v", child, commandError)
			return
		}
		if strings.Join(command, " ") != standInChild {
			// A different command means the id was reused.
			return
		}
		// Ignored: the child ending on its own between the look and the kill is the outcome the kill asks for.
		_ = syscall.Kill(child, syscall.SIGKILL)
	})
}

// TestAStandInBinarysChildIsKilledWhenItsTestEnds: a child a test leaves running is gone once the test's
// cleanups have run, so a stand-in cohere-swift test that fails early or hangs leaves no sleep behind.
func TestAStandInBinarysChildIsKilledWhenItsTestEnds(test *testing.T) {
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
	// The subtest stands for a stand-in cohere-swift test that leaves its child running; its cleanups run as it ends.
	// Not parallel: the wait below needs its cleanup to have killed the child, and a parallel subtest would not
	// start until this test returned
	cleaned := test.Run("a test that leaves its child running", func(test *testing.T) {
		killChildAtCleanup(test, childFile)
	})
	if !cleaned {
		// The cleanup said why it left the child running, so this test ends its own sleep, which no other process
		// can hold the id of while this one hasn't waited on it, rather than wait on it for go test's -timeout.
		// Ignored: the sleep ending on its own first is what the kill asks for.
		_ = sleeper.Process.Kill()
		<-ended
		test.FailNow()
	}
	// No deadline: a cleanup that said nothing and did not kill the child leaves this waiting on a sleep that outlasts
	// any run, a hang go test's -timeout fails.
	<-ended
}
