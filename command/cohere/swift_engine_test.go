package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSwiftEngine writes a shell script standing in for cohere-swift, so the process boundary can be driven
// through every way an engine ends without building the real one.
func fakeSwiftEngine(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cohere-swift")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func runFakeSwiftEngine(t *testing.T, body string, mode swiftMode) (string, string, int, error) {
	t.Helper()
	var out, standardError bytes.Buffer
	exitCode, err := runEngineBinary(fakeSwiftEngine(t, body), []string{"--contract", "1"}, newSwiftRun(&out, mode, "", time.Now()), &standardError)
	return out.String(), standardError.String(), exitCode, err
}

func TestRunEngineBinaryEndings(t *testing.T) {
	fixtures, err := filepath.Abs(filepath.Join("..", "..", "swift", "Contract"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a real stream streams and exits with its summary", func(t *testing.T) {
		out, _, exitCode, err := runFakeSwiftEngine(t, "cat '"+filepath.Join(fixtures, "Findings.jsonl")+"'; exit 1", swiftModeCheck)
		if err != nil || exitCode != 1 {
			t.Fatalf("exit %d, err %v\n%s", exitCode, err, out)
		}
		if !strings.Contains(out, "[cohere-swift/no-force-unwrap/forceUnwrap]") || !strings.Contains(out, "phases: ") {
			t.Errorf("the stream was not rendered:\n%s", out)
		}
	})

	t.Run("the control: a clean stream exits 0", func(t *testing.T) {
		_, _, exitCode, err := runFakeSwiftEngine(t, "cat '"+filepath.Join(fixtures, "Clean.jsonl")+"'", swiftModeCheck)
		if err != nil || exitCode != 0 {
			t.Fatalf("a clean engine run exited %d, err %v", exitCode, err)
		}
	})

	t.Run("exit 2 passes stderr through and says nothing was checked", func(t *testing.T) {
		_, standardError, exitCode, err := runFakeSwiftEngine(t, "echo 'cohere-swift: --timing is not implemented for Swift yet' >&2; exit 2", swiftModeCheck)
		if err == nil || exitCode != 1 || !strings.Contains(err.Error(), "did not run, so nothing was checked") {
			t.Fatalf("exit %d, err %v", exitCode, err)
		}
		if !strings.Contains(standardError, "--timing is not implemented") {
			t.Errorf("the engine's own sentence was not passed through: %q", standardError)
		}
	})

	t.Run("a signal is named as a signal, never as an exit code", func(t *testing.T) {
		out, _, exitCode, err := runFakeSwiftEngine(t, "head -2 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; kill -9 $$", swiftModeCheck)
		if err == nil || exitCode != 1 {
			t.Fatalf("a killed engine exited %d, err %v", exitCode, err)
		}
		if !strings.Contains(err.Error(), "was killed by") {
			t.Errorf("the signal was not named: %v", err)
		}
		if strings.Contains(err.Error(), "exited -1") {
			t.Errorf("printed an exit code the process never chose: %v", err)
		}
		if !strings.Contains(out, "did not run (the Swift engine was killed") {
			t.Errorf("the phase line did not say what was not reached:\n%s", out)
		}
	})

	t.Run("a broken record kills the engine rather than waiting on it", func(t *testing.T) {
		start := time.Now()
		_, _, exitCode, err := runFakeSwiftEngine(t, "head -1 '"+filepath.Join(fixtures, "Clean.jsonl")+"'; echo 'Compiling swift-syntax'; sleep 30", swiftModeCheck)
		if err == nil || exitCode != 1 || !strings.Contains(err.Error(), "the Swift engine is broken") {
			t.Fatalf("exit %d, err %v", exitCode, err)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("waited %s for an engine that had already broken its contract", elapsed)
		}
	})
}
