package replace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var errHeld = errors.New("the destination is held open")

// A rename refused while the destination is held succeeds once it is let go, and one refused for any
// other reason fails at once. Windows refuses the way the fake below does; nothing else does.
func TestARenameRefusedWhileHeldIsRetriedUntilItIsLetGo(test *testing.T) {
	test.Parallel()
	refusals := 3
	rename := func(string, string) error {
		if refusals > 0 {
			refusals--
			return errHeld
		}
		return nil
	}
	slept := time.Duration(0)
	sleep := func(pause time.Duration) { slept += pause }
	held := func(err error) bool { return errors.Is(err, errHeld) }

	if replacingError := replacing(rename, held, sleep, "from", "to"); replacingError != nil {
		test.Fatalf("a rename refused three times and then allowed failed: %v", replacingError)
	}
	if refusals != 0 || slept == 0 {
		test.Fatalf("it did not retry: %d refusals left, slept %s", refusals, slept)
	}

	other := errors.New("no such directory")
	calls := 0
	failing := func(string, string) error { calls++; return other }
	if replacingError := replacing(
		failing,
		held,
		sleep,
		"from",
		"to",
	); !errors.Is(replacingError, other) || calls != 1 {
		test.Fatalf(
			"a rename refused for another reason was retried (%d calls) or lost its error: %v",
			calls,
			replacingError,
		)
	}
}

// A destination held for good fails after the bound with the refusal itself, rather than waiting forever.
func TestARenameHeldForGoodFailsAfterTheBound(test *testing.T) {
	test.Parallel()
	slept := time.Duration(0)
	replacingError := replacing(func(string, string) error { return errHeld }, func(error) bool { return true },
		func(pause time.Duration) { slept += pause }, "from", "to")
	if !errors.Is(replacingError, errHeld) {
		test.Fatalf("a held destination did not fail with its refusal: %v", replacingError)
	}
	if slept < retryBound || slept > retryBound+200*time.Millisecond {
		test.Fatalf("waited %s, want about %s", slept, retryBound)
	}
}

// File replaces a file, on every platform, including one another handle in this process has open: on
// Windows that is the refusal it retries through, released here partway into the bound.
func TestFileReplacesAFileHeldOpenBriefly(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	destination := filepath.Join(directory, "table.gob")
	source := filepath.Join(directory, ".table-new")
	if writeError := os.WriteFile(destination, []byte("old"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if writeError := os.WriteFile(source, []byte("new"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	reader, openError := os.Open(destination)
	if openError != nil {
		test.Fatal(openError)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		// Ignored: the file was only opened to hold it, never written, so a failed close loses nothing.
		reader.Close()
	}()
	if fileError := File(source, destination); fileError != nil {
		test.Fatalf("replacing a file held open for 100ms failed: %v", fileError)
	}
	contents, readError := os.ReadFile(destination)
	if readError != nil {
		test.Fatal(readError)
	}
	if string(contents) != "new" {
		test.Fatalf("the destination holds %q", contents)
	}
}
