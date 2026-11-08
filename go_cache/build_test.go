package go_cache

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Positive control on a real cache: the go command fills it, a trim to nothing removes every entry, and
// the go command opens the trimmed cache and builds again with no error.
func TestTheGoCommandBuildsFromATrimmedCache(test *testing.T) {
	test.Parallel()
	if _, lookPathError := exec.LookPath("go"); lookPathError != nil {
		test.Skip("no go command")
	}
	cache := test.TempDir()
	module := test.TempDir()
	if writeError := os.WriteFile(
		filepath.Join(module, "go.mod"),
		[]byte("module trimmed\n\ngo 1.21\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	if writeError := os.WriteFile(
		filepath.Join(module, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	build := func() {
		test.Helper()
		command := exec.Command("go", "build", "-o", filepath.Join(test.TempDir(), "trimmed"), ".")
		command.Dir = outsideModule(test, module)
		command.Env = append(os.Environ(), "GOCACHE="+cache, "GOFLAGS=", "GOWORK=off")
		if output, outputError := command.CombinedOutput(); outputError != nil {
			test.Fatalf("go build: %v: %s", outputError, output)
		}
	}
	build()

	// A day from now nothing is in use, so a target of zero takes every entry.
	trimmed, trimError := Trim(cache, Limit{Cap: 0, Target: 0, Spare: MinimumSpare}, time.Now().Add(24*time.Hour))
	if trimError != nil {
		test.Fatal(trimError)
	}
	if trimmed.Removed == 0 || trimmed.After != 0 {
		test.Fatalf("trimmed %+v, want every entry gone", trimmed)
	}
	build()
}

// outsideModule returns directory, a place a test runs the go command, after failing the test unless
// it lies outside the module the test runs in, symbolic links followed. Go keys a cached test result
// only on files inside that module, so a go command run on a module the test wrote somewhere of its
// own reads nothing the cache could miss. It is cohere's build_inputs.OutsideModule, which this
// module cannot import, as go_listing's is; cohere's guard accepts a go command whose directory is set
// from it, here in a nested module alone, and this makes that claim checked rather than trusted.
func outsideModule(test testing.TB, directory string) string {
	test.Helper()
	workingDirectory, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		test.Fatal(workingDirectoryError)
	}
	module := workingDirectory
	for {
		if _, statError := os.Stat(filepath.Join(module, "go.mod")); statError == nil {
			break
		}
		parent := filepath.Dir(module)
		if parent == module {
			test.Fatalf("no go.mod above %s, so there is no module to be outside of", workingDirectory)
		}
		module = parent
	}
	resolvedModule, moduleError := filepath.EvalSymlinks(module)
	if moduleError != nil {
		test.Fatal(moduleError)
	}
	absolute, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	resolved, resolveError := filepath.EvalSymlinks(absolute)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	relative, relativeError := filepath.Rel(resolvedModule, resolved)
	if relativeError != nil {
		test.Fatal(relativeError)
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		test.Fatalf(
			"%s is inside the module at %s, so the go command run there reads this module's code",
			directory,
			resolvedModule,
		)
	}
	return directory
}
