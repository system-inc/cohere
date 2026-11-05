//go:build unix

// The stand-in engines here are /bin/sh scripts, and the process-group behavior they prove exists only
// on Unix, so the file builds there.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeSwiftEngine writes a shell script standing in for cohere-swift, so the process boundary can be driven
// through every way an engine ends without building the real one.
func fakeSwiftEngine(test *testing.T, body string) string {
	test.Helper()
	path := filepath.Join(test.TempDir(), "cohere-swift")
	if writeError := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	return path
}

func runFakeSwiftEngine(test *testing.T, body string, mode engineMode) (string, string, int, error) {
	test.Helper()
	var out, standardError bytes.Buffer
	run := newEngineRun(&out, mode, swiftLanguage, "", time.Now())
	exitCode, runEngineBinaryError := runEngineBinary(fakeSwiftEngine(test, body), []string{"--contract", "1"}, run, &standardError)
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
		out, _, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(test, "cat '"+filepath.Join(fixtures, "Findings.jsonl")+"'; exit 1", engineModeCheck)
		if runFakeSwiftEngineError != nil || exitCode != 1 {
			test.Fatalf("exit %d, err %v\n%s", exitCode, runFakeSwiftEngineError, out)
		}
		if !strings.Contains(out, "[cohere-swift/force-unwrapping/forceUnwrap]") || !strings.Contains(out, "phases: ") {
			test.Errorf("the stream was not rendered:\n%s", out)
		}
	})

	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("the control: a clean stream exits 0", func(test *testing.T) {
		_, _, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(test, "cat '"+filepath.Join(fixtures, "Clean.jsonl")+"'", engineModeCheck)
		if runFakeSwiftEngineError != nil || exitCode != 0 {
			test.Fatalf("a clean engine run exited %d, err %v", exitCode, runFakeSwiftEngineError)
		}
	})

	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("exit 2 passes stderr through and says nothing was checked", func(test *testing.T) {
		_, standardError, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(test, "echo 'cohere-swift: --timing is not implemented for Swift yet' >&2; exit 2", engineModeCheck)
		if runFakeSwiftEngineError == nil || exitCode != 1 || !strings.Contains(runFakeSwiftEngineError.Error(), "did not run, so nothing was checked") {
			test.Fatalf("exit %d, err %v", exitCode, runFakeSwiftEngineError)
		}
		if !strings.Contains(standardError, "--timing is not implemented") {
			test.Errorf("the engine's own sentence was not passed through: %q", standardError)
		}
	})

	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("a signal is named as a signal, never as an exit code", func(test *testing.T) {
		out, _, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(test, "head -2 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; kill -9 $$", engineModeCheck)
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
		start := time.Now()
		_, _, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(test, "head -1 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; echo 'Compiling swift-syntax'; sleep 30", engineModeCheck)
		if runFakeSwiftEngineError == nil || exitCode != 1 || !strings.Contains(runFakeSwiftEngineError.Error(), "the Swift engine is broken") {
			test.Fatalf("exit %d, err %v", exitCode, runFakeSwiftEngineError)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			test.Errorf("waited %s for an engine that had already broken its contract", elapsed)
		}
	})

	// The child is started before the broken record, so it is always alive when the engine is killed.
	// Killing only the engine left it holding the pipes, and the front door waited out its 30 seconds.
	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("a broken engine's children die with it", func(test *testing.T) {
		childFile := filepath.Join(test.TempDir(), "child")
		start := time.Now()
		_, _, exitCode, runFakeSwiftEngineError := runFakeSwiftEngine(test,
			"sleep 30 & echo $! > '"+childFile+"'; head -1 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; echo 'Compiling swift-syntax'; wait",
			engineModeCheck)
		if runFakeSwiftEngineError == nil || exitCode != 1 || !strings.Contains(runFakeSwiftEngineError.Error(), "the Swift engine is broken") {
			test.Fatalf("exit %d, err %v", exitCode, runFakeSwiftEngineError)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			test.Errorf("waited %s on a child of an engine that had already broken its contract", elapsed)
		}
		requireProcessEnded(test, childFile)
	})

	// `SIGTERM` rather than an interrupt, because a POSIX shell starts a background job with interrupts
	// ignored, and the stand-in's child must be one the signal can end.
	// Not parallel: it drives a check run through engineRun, whose record acceptors set the package-level activeSummary
	test.Run("a signal cohere receives is passed to the engine and everything it started", func(test *testing.T) {
		childFile := filepath.Join(test.TempDir(), "child")
		interrupts := make(chan os.Signal, 1)
		var out, standardError bytes.Buffer
		run := newEngineRun(&out, engineModeCheck, swiftLanguage, "", time.Now())
		go func() {
			waitForFile(test, childFile)
			interrupts <- syscall.SIGTERM
		}()
		start := time.Now()
		exitCode, runEngineBinaryUntilError := runEngineBinaryUntil(
			fakeSwiftEngine(test, "sleep 30 & echo $! > '"+childFile+"'; head -2 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; wait"),
			[]string{"--contract", "1"}, run, &standardError, interrupts)
		if runEngineBinaryUntilError == nil || exitCode != 1 || !strings.Contains(runEngineBinaryUntilError.Error(), "was killed by") {
			test.Fatalf("an engine signalled through cohere exited %d, err %v\n%s", exitCode, runEngineBinaryUntilError, out.String())
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			test.Errorf("the signal took %s to end the engine, so it was not passed on", elapsed)
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
	contents, err := os.ReadFile(path)
	if err != nil {
		test.Fatalf("the stand-in engine never wrote its child's id: %v", err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil {
		test.Fatalf("the child id %q is not a number: %v", contents, err)
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(child, 0) == syscall.ESRCH {
			return
		}
	}
	_ = syscall.Kill(child, syscall.SIGKILL)
	test.Errorf("the engine's child %d outlived it", child)
}
