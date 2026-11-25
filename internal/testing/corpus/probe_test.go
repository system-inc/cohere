package corpus

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

// runProbe runs the test named name again in a child process with the Ahra corpus's variable set to
// value and COHERE_CORPUS_PROBE set to probe, and returns its verbose output.
func runProbe(test *testing.T, name, probe, value string) string {
	test.Helper()
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	command := exec.Command(executable, "-test.run", "^"+name+"$", "-test.v")
	command.Env = append(os.Environ(), "COHERE_CORPUS_PROBE="+probe, Ahra.Variable+"="+value)
	output, runError := command.CombinedOutput()
	// A child that ran and failed is a verdict the caller reads from the output, since a probe that expects
	// `--- FAIL` exits nonzero. A child that never ran has no verdict to read.
	var exitError *exec.ExitError
	if runError != nil && !errors.As(runError, &exitError) {
		test.Fatalf("running the probe %s: %v", name, runError)
	}
	return string(output)
}
