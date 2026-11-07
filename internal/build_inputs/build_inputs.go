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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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

// listedPackage is the part of `go list -json` that names what a package's build reads. A field whose name the
// house spells apart from go list's is tagged with go list's.
type listedPackage struct {
	Directory         string `json:"Dir"`
	Standard          bool
	GoFiles           []string
	CgoFiles          []string
	CFiles            []string
	CxxFiles          []string `json:"CXXFiles"`
	MFiles            []string
	HFiles            []string
	FFiles            []string
	SFiles            []string
	SwigFiles         []string
	SwigCxxFiles      []string `json:"SwigCXXFiles"`
	SysoFiles         []string
	IgnoredGoFiles    []string
	IgnoredOtherFiles []string
	EmbedFiles        []string
	Module            *struct {
		GoMod string
	}
}

// listedFields is listedPackage's fields as go list's -json takes them, so it prints nothing else.
const listedFields = "Dir,Standard,GoFiles,CgoFiles,CFiles,CXXFiles,MFiles,HFiles,FFiles,SFiles,SwigFiles," +
	"SwigCXXFiles,SysoFiles,IgnoredGoFiles,IgnoredOtherFiles,EmbedFiles,Module"

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
// "-tags", "cohere_planted_panic", ".". It returns how many files it opened.
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
		for _, files := range [][]string{
			listed.GoFiles,
			listed.CgoFiles,
			listed.CFiles,
			listed.CxxFiles,
			listed.MFiles,
			listed.HFiles,
			listed.FFiles,
			listed.SFiles,
			listed.SwigFiles,
			listed.SwigCxxFiles,
			listed.SysoFiles,
			listed.IgnoredGoFiles,
			listed.IgnoredOtherFiles,
			listed.EmbedFiles,
		} {
			for _, file := range files {
				paths = append(paths, filepath.Join(listed.Directory, file))
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
	for _, path := range paths {
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

func isSumFile(path string) bool {
	return strings.HasSuffix(path, "go.sum") || strings.HasSuffix(path, "go.work.sum")
}

// list is `go list -deps` of the build's packages, with its flags, from directory.
func list(directory string, arguments []string) ([]listedPackage, error) {
	command := exec.Command("go", append([]string{"list", "-deps", "-json=" + listedFields}, arguments...)...)
	command.Dir = directory
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if runError := command.Run(); runError != nil {
		return nil, fmt.Errorf("go list: %w\n%s", runError, stderr.String())
	}
	packages := []listedPackage{}
	decoder := json.NewDecoder(&stdout)
	for {
		var listed listedPackage
		decodeError := decoder.Decode(&listed)
		if errors.Is(decodeError, io.EOF) {
			return packages, nil
		}
		if decodeError != nil {
			return nil, fmt.Errorf("reading go list's output: %w", decodeError)
		}
		packages = append(packages, listed)
	}
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
