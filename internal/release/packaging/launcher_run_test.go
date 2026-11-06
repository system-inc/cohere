package release

import (
	"strings"
	"testing"
)

// launcherRun is one run's outcome.
type launcherRun struct {
	exitCode       int
	standardOutput string
	standardError  string
}

func (result launcherRun) requireRan(test *testing.T, argument string) {
	test.Helper()
	if result.exitCode != 0 || !strings.Contains(result.standardOutput, "fixture ran: "+argument) {
		test.Fatalf(
			"the launcher did not run the intact binary: exit %d\nstdout: %s\nstderr: %s",
			result.exitCode,
			result.standardOutput,
			result.standardError,
		)
	}
}

// requireRefused holds a refusal to three things: a non-zero exit, a message naming the file and the
// reason, and nothing run, which the tampered fixture would have announced in either case.
func (result launcherRun) requireRefused(test *testing.T, file string, reason string) {
	test.Helper()
	if result.exitCode == 0 {
		test.Fatalf(
			"the launcher exited zero over %s\nstdout: %s\nstderr: %s",
			file,
			result.standardOutput,
			result.standardError,
		)
	}
	if strings.Contains(strings.ToLower(result.standardOutput), "fixture ran") {
		test.Fatalf("the launcher refused, but only after running the binary:\n%s", result.standardOutput)
	}
	if !strings.Contains(result.standardError, file) || !strings.Contains(result.standardError, reason) {
		test.Fatalf("the refusal does not name %s and say %q:\n%s", file, reason, result.standardError)
	}
}
