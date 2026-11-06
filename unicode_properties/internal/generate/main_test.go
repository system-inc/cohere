package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// checkChildVariable tells a test binary run by TestCheckReportsAnUnreadableOutput to be the generator, checking
// the tables file it names.
const checkChildVariable = "COHERE_UNICODE_GENERATE_CHECK_CHILD"

// A check whose tables file cannot be read says which file and exits 1, rather than panicking with a stack trace
// and exit status 2. The test binary runs itself as the generator with -check against a file that does not exist.
// The child is a process of its own running this test alone, so the flags main defines are no other test's, and
// the test runs in parallel.
func TestCheckReportsAnUnreadableOutput(test *testing.T) {
	test.Parallel()
	if missing := os.Getenv(checkChildVariable); missing != "" {
		os.Args = []string{"generate", "-check", "-output", missing}
		main()
		return
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	missing := filepath.Join(test.TempDir(), "tables_generated.go")
	command := exec.Command(executable, "-test.run=^TestCheckReportsAnUnreadableOutput$")
	command.Env = append(os.Environ(), checkChildVariable+"="+missing)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	runError := command.Run()
	var exitError *exec.ExitError
	if !errors.As(runError, &exitError) || exitError.ExitCode() != 1 {
		test.Fatalf("the check of a missing tables file ended with %v, want exit status 1:\n%s", runError,
			stderr.String())
	}
	text := stderr.String()
	if !strings.Contains(text, "reading "+missing+": ") || strings.Contains(text, "goroutine ") {
		test.Fatalf("the check of a missing tables file did not report it in one line:\n%s", text)
	}
}
