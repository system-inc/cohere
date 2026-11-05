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

// failingChildVariable tells a test binary run by TestAPackageThatCannotBeWrittenSaysWhyAndExitsOne to be the
// command, staging into the output directory the variable names.
const failingChildVariable = "BUILD_DISPATCHER_PACKAGE_CHILD_OUTPUT"

// A package directory that cannot be created ends the command with its reason and exit code 1, where it once
// panicked with a stack trace and exit code 2. The test binary runs itself as the command, pointed at an output
// path that is a file, so nothing can be created under it. The child is a process of its own running this test
// alone, so the arguments it sets are shared with no other test.
func TestAPackageThatCannotBeWrittenSaysWhyAndExitsOne(test *testing.T) {
	test.Parallel()
	if output := os.Getenv(failingChildVariable); output != "" {
		os.Args = []string{"build_dispatcher_package", output}
		main()
		return
	}
	output := filepath.Join(test.TempDir(), "a-file")
	if writeError := os.WriteFile(output, []byte("not a directory\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	command := exec.Command(executable, "-test.run=^TestAPackageThatCannotBeWrittenSaysWhyAndExitsOne$")
	command.Env = append(os.Environ(), failingChildVariable+"="+output)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	runError := command.Run()
	var exitError *exec.ExitError
	if !errors.As(runError, &exitError) || exitError.ExitCode() != 1 {
		test.Fatalf("the command ended with %v, want exit code 1:\n%s", runError, stderr.String())
	}
	text := stderr.String()
	if !strings.Contains(text, "creating the package directory: ") || strings.Contains(text, "panic:") {
		test.Fatalf("the command did not say why it stopped, or panicked:\n%s", text)
	}
}
