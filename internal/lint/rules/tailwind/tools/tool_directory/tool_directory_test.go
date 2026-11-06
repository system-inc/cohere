package tool_directory

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The directory is found from either form runtime.Caller gives, and both forms name the same place.
//
// The trimmed form is written out rather than taken from runtime.Caller, so the test covers it on a
// machine that builds without -trimpath too, and the caller's own form covers whichever this build has.
func TestOfFindsTheSourceDirectoryFromEitherForm(test *testing.T) {
	test.Parallel()
	want, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		test.Fatal(workingDirectoryError)
	}
	_, callerPath, _, _ := runtime.Caller(0)
	for _, form := range []string{
		callerPath,
		filepath.Join(want, "tool_directory_test.go"),
		"github.com/system-inc/cohere/internal/lint/rules/tailwind/tools/tool_directory/tool_directory_test.go",
	} {
		got, toolDirectoryOfError := Of(form)
		if toolDirectoryOfError != nil {
			test.Fatalf("%s: %v", form, toolDirectoryOfError)
		}
		if got != want {
			test.Errorf("%s resolved to %s, want %s", form, got, want)
		}
	}
}

// A path naming a file that is not there is refused rather than answered with a directory nobody
// chose, which is what filepath.Dir of a trimmed path used to be.
func TestOfRefusesADirectoryWithoutTheFile(test *testing.T) {
	test.Parallel()
	if directory, toolDirectoryOfError := Of(filepath.Join(test.TempDir(), "caller.go")); toolDirectoryOfError == nil {
		test.Errorf("a directory without the caller's file was accepted: %s", directory)
	}
	if directory, toolDirectoryOfError := Of(
		"github.com/system-inc/cohere/no/such/package/caller.go",
	); toolDirectoryOfError == nil {
		test.Errorf("an import path that resolves nowhere was accepted: %s", directory)
	}
}
