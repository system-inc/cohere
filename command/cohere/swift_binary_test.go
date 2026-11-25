//go:build unix

// The stand-in children here are /bin/sh scripts, and the process-group behavior they prove exists only
// on Unix, so the file builds there.

package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
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
		// The child sleeps until this test ends, so a front door that waited on it hangs, a hang go test's -timeout
		// fails. The sleep is this test binary's own child (own_child_test.go), and ends with the test.
		_, _, exitCode, runFakeSwiftBinaryError := runFakeSwiftBinary(
			test,
			"head -1 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; echo 'Compiling swift-syntax'; "+
				ownChildShellCommand(test, ownChildSleep, ownChildLifeline(test)),
			contractModeCheck,
		)
		if runFakeSwiftBinaryError == nil ||
			exitCode != 1 ||
			!strings.Contains(runFakeSwiftBinaryError.Error(), "cohere-swift is broken") {
			test.Fatalf("exit %d, err %v", exitCode, runFakeSwiftBinaryError)
		}
	})

	// The child is started before the broken record, so it is always alive when the child is killed.
	// Killing only the child left it holding the pipes, and the front door waited on it. It sleeps until this
	// test ends, so a front door that waits on it again hangs, a hang go test's -timeout fails, and the test's
	// lifeline ends it however the test went.
	// Not parallel: it drives a check run through contractRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("a broken child's children die with it", func(test *testing.T) {
		childFile := filepath.Join(test.TempDir(), "child")
		_, _, exitCode, runFakeSwiftBinaryError := runFakeSwiftBinary(test,
			ownChildShellCommand(test, ownChildSleep, ownChildLifeline(test))+" & echo $! > '"+
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
	// until this test ends, so a signal not passed on leaves the run waiting, a hang go test's -timeout fails.
	// Not parallel: it drives a check run through contractRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("a signal cohere receives is passed to the child and everything it started", func(test *testing.T) {
		childFile := filepath.Join(test.TempDir(), "child")
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
				ownChildShellCommand(test, ownChildSleep, ownChildLifeline(test))+" & echo $! > '"+childFile+
					"'; head -2 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; wait",
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
// orphan is reaped by launchd or init a moment after it dies, so it is given two seconds. One that outlived its
// parent is not killed here, by an id read from a file: the lifeline it was handed ends it when its test does.
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
	test.Errorf("the child's child %d outlived it", child)
}

// TestAStandInBinarysChildEndsWhenItsTestEnds: a child a test leaves running is gone once the test's cleanups have
// run, so a stand-in cohere-swift test that fails early or hangs leaves no child behind. The child is this test
// binary, handed the subtest's lifeline as a stand-in's child is, and nothing signals it: the lifeline's removal
// ends it, with an exit of its own.
func TestAStandInBinarysChildEndsWhenItsTestEnds(test *testing.T) {
	test.Parallel()
	ended := make(chan error, 1)
	// The subtest stands for a stand-in cohere-swift test that leaves its child running; its cleanups run as it ends.
	// The child's command is this test's, so the kill its cleanup holds back for a run that stopped early is this
	// test's too, and comes after the wait below.
	// Not parallel: the wait below needs its cleanup to have ended the child, and a parallel subtest would not
	// start until this test returned
	started := test.Run("a test that leaves its child running", func(subtest *testing.T) {
		sleeper := ownChild(test, ownChildSleep, ownChildLifeline(subtest))
		if startError := sleeper.Start(); startError != nil {
			subtest.Fatal(startError)
		}
		go func() { ended <- sleeper.Wait() }()
	})
	if !started {
		return
	}
	// No deadline: a cleanup that did not end the child leaves this waiting on a sleep that outlasts go test's
	// -timeout, which fails the hang.
	if waitError := <-ended; waitError != nil {
		test.Errorf("the child ended with %v, want an exit of its own once its lifeline was gone", waitError)
	}
}
