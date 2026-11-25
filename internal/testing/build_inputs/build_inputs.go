// Package build_inputs makes Go's test cache see a binary a test builds and runs (#4p82x8d).
//
// Go keys a test's cached result on the linked test binary and on the files and environment variables the test
// process itself opens and reads. A test that builds one of our commands with a `go build` subprocess and judges
// that binary depends on every source file the subprocess reads, and Go sees none of them. An edit that only the
// built binary reaches is code the test binary's linker drops as dead, so the test binary is byte-identical, and
// Go replays the last pass without running the test: on 2026-11-07 a mutant of command/cohere's json_output.go
// read `ok (cached)` under `go test` and cohere-dev test alike, and so would the landing gate.
//
// Record, called before the build, opens in the test's own process every file the build reads and reads every
// environment variable that shapes it, so each one joins the cache key: an edit to any of them is a miss.
package build_inputs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/modules/go_listing"
)

// buildVariables are the environment variables that change what a go build of our code produces. Each is read
// with os.Getenv, set or not, so Go's test cache keys on its value.
var buildVariables = []string{
	"CGO_ENABLED",
	"GOARCH",
	"GOARM64",
	"GOAMD64",
	"GOEXPERIMENT",
	"GOFLAGS",
	"GOOS",
	"GOTOOLCHAIN",
	"GOWORK",
}

// listedFields are the fields of a go_listing.Package that name what a package's build reads, asked of go list so
// it prints nothing else.
var listedFields = []string{
	"Dir",
	"Standard",
	"GoFiles",
	"CgoFiles",
	"CFiles",
	"CXXFiles",
	"MFiles",
	"HFiles",
	"FFiles",
	"SFiles",
	"SwigFiles",
	"SwigCXXFiles",
	"SysoFiles",
	"IgnoredGoFiles",
	"IgnoredOtherFiles",
	"EmbedFiles",
	"Module",
}

// listedFiles are the lists a build reads: every list go_listing.BuildFileLists names, and the files a build
// constraint leaves out, so no build tag leaves a file out of the key.
var listedFiles = append(go_listing.BuildFileLists(), go_listing.IgnoredGoFiles, go_listing.IgnoredOtherFiles)

// recording is one Record of a build, made once per test process however many tests ask for it: what a process
// opened once is in its test log for the whole run.
type recording struct {
	once    sync.Once
	opened  int
	failure error
}

var recordings sync.Map

// Record opens, in the calling test's process, every file a go build of arguments run from directory reads, and
// reads the environment variables that shape that build, so Go's test cache keys the test's result on all of
// them. arguments are what the build is given after `go build`, less its -o: its flags and its packages, such as
// "-tags", "cohere_planted_panic", ".". For a `go test`, add -test, so the test files and what they import are
// listed too. It returns how many files it opened.
//
// Opened are every non-standard package's directory, whose listing Go hashes with each entry's size and
// modification time, so an added or removed file is a miss too; each of its Go, cgo, assembly, embedded and
// build-ignored files, the last so no build tag leaves a file out; and the go.mod, go.sum, go.work and go.work.sum
// the build resolves through. A standard package changes only with the toolchain, which the test binary carries.
// Go rechecks only files inside the test's own module, so a module from the module cache is covered by the go.sum
// that pins it.
func Record(test testing.TB, directory string, arguments ...string) int {
	test.Helper()
	key := directory + "\x00" + strings.Join(arguments, "\x00")
	value, _ := recordings.LoadOrStore(key, &recording{})
	entry, isRecording := value.(*recording)
	if !isRecording {
		// Invariant: recordings holds nothing but what this function stores in it.
		test.Fatalf("recordings holds a %T", value)
	}
	entry.once.Do(func() {
		entry.opened, entry.failure = record(directory, arguments)
	})
	if entry.failure != nil {
		test.Fatalf(
			"recording what go build %s reads in %s: %v",
			strings.Join(arguments, " "),
			directory,
			entry.failure,
		)
	}
	return entry.opened
}

func record(directory string, arguments []string) (int, error) {
	for _, name := range buildVariables {
		os.Getenv(name)
	}
	packages, listError := list(directory, arguments)
	if listError != nil {
		return 0, listError
	}
	paths := []string{}
	modules := map[string]bool{}
	for _, listed := range packages {
		if listed.Standard {
			continue
		}
		paths = append(paths, listed.Directory)
		for _, file := range listed.SourceFiles(listedFiles) {
			// A test main go generates is named by its path in the build cache: go's own, not ours.
			if !filepath.IsAbs(file) {
				paths = append(paths, listed.Path(file))
			}
		}
		if listed.Module != nil && listed.Module.GoMod != "" && !modules[listed.Module.GoMod] {
			modules[listed.Module.GoMod] = true
			paths = append(paths, listed.Module.GoMod, filepath.Join(filepath.Dir(listed.Module.GoMod), "go.sum"))
		}
	}
	workspace, workspaceError := goEnvironment(directory, "GOWORK")
	if workspaceError != nil {
		return 0, workspaceError
	}
	if workspace != "" && workspace != "off" {
		paths = append(paths, workspace, workspace+".sum")
	}
	opened := 0
	// With -test a package is listed again as its test variant, so its files come twice.
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		file, openError := os.Open(path)
		if errors.Is(openError, os.ErrNotExist) && isSumFile(path) {
			// A module with no requirements has no go.sum, and a workspace no go.work.sum. The failed open is
			// logged all the same, so one that appears later is a miss.
			continue
		}
		if openError != nil {
			return opened, fmt.Errorf("opening %s: %w", path, openError)
		}
		opened++
		if closeError := file.Close(); closeError != nil {
			return opened, fmt.Errorf("closing %s: %w", path, closeError)
		}
	}
	return opened, nil
}

// OutsideModule returns directory, a place a test runs the go command, after failing the test unless it lies
// outside the module the test belongs to, symbolic links followed. Go rechecks only files inside that module, so a
// go command run on a module the test wrote somewhere of its own reads nothing the cache could miss, and needs no
// Record; one that runs inside the module reads our code, and does. The guard in internal/testing/guard accepts a go
// command whose directory is set from this in place of a Record, and this makes that claim checked rather than
// trusted.
func OutsideModule(test testing.TB, directory string) string {
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
			"%s is inside the module at %s, so the go command run there reads our code: call Record for it",
			directory,
			resolvedModule,
		)
	}
	return directory
}

func isSumFile(path string) bool {
	return strings.HasSuffix(path, "go.sum") || strings.HasSuffix(path, "go.work.sum")
}

// list is `go list -deps` of the build's packages, with its flags, from directory. The build's arguments go after
// the listing's own flags as they are, flags and packages alike, which is how go list takes a build's.
func list(directory string, arguments []string) ([]go_listing.Package, error) {
	listArguments, argumentsError := go_listing.Arguments(go_listing.Request{
		Dependencies: true,
		Fields:       listedFields,
		BuildFlags:   arguments,
	})
	if argumentsError != nil {
		return nil, argumentsError
	}
	command := exec.Command("go", listArguments...)
	command.Dir = directory
	return go_listing.Run(command)
}

// goEnvironment is one of go env's values, as a build from directory sees it.
func goEnvironment(directory string, name string) (string, error) {
	command := exec.Command("go", "env", name)
	command.Dir = directory
	output, outputError := command.Output()
	if outputError != nil {
		return "", fmt.Errorf("go env %s: %w", name, outputError)
	}
	return strings.TrimSpace(string(output)), nil
}
