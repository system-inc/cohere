// Command generate writes the website's data (internal/docsdata) into the module's docs/data/ directory.
// Run it from the module root:
//
//	go run ./internal/docsdata/tools/generate            write the files
//	go run ./internal/docsdata/tools/generate -check     exit 1 if any is stale, writing nothing
//	go run ./internal/docsdata/tools/generate -capture   rerun the lint tests to recapture the examples first
//
// cli.json is read from the binary's own help, so every run builds ./command/cohere into a temporary
// directory. The examples come from the committed files unless -capture reruns the lint tests with
// COHERE_DOCS_CAPTURE set, which takes minutes and is only needed when a rule's tests change.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/docsdata"
	"github.com/system-inc/cohere/internal/docsdata/capture"
)

// capturedPackages are the tests that assert rule cases through the harness.
const capturedPackages = "./internal/lint/..."

// verbs are the subcommands with a flag set of their own.
var verbs = []string{"rename"}

func main() {
	check := flag.Bool("check", false, "report stale files and exit 1, writing nothing")
	recapture := flag.Bool("capture", false, "rerun the lint tests with "+capture.Variable+" set and pick the examples afresh")
	flag.Parse()

	if err := run(*check, *recapture); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(check bool, recapture bool) error {
	scratch, err := os.MkdirTemp("", "cohere-docsdata-")
	if err != nil {
		return err
	}
	defer removeScratch(scratch)

	inputs, err := gather(scratch, recapture)
	if err != nil {
		return err
	}
	files, err := docsdata.Build(inputs)
	if err != nil {
		return err
	}

	if check {
		stale, err := docsdata.Stale(".", files)
		if err != nil {
			return err
		}
		for _, path := range stale {
			fmt.Printf("stale: %s\n", path)
		}
		if len(stale) > 0 {
			os.Exit(1)
		}
		return nil
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		existing, _ := os.ReadFile(path)
		if bytes.Equal(existing, files[path]) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, files[path], 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", path)
	}
	orphans, err := docsdata.OrphanedExamples(".", files)
	if err != nil {
		return err
	}
	for _, path := range orphans {
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", path)
	}
	return nil
}

// gather reads every source Build takes.
func gather(scratch string, recapture bool) (docsdata.Inputs, error) {
	inputs, err := docsdata.SourceInputs(".")
	if err != nil {
		return inputs, err
	}
	if inputs.Help, inputs.VerbHelp, err = binaryHelp(scratch); err != nil {
		return inputs, err
	}
	if !recapture {
		return inputs, nil
	}

	// No -count=1, as no gate run passes it: Go keys a cached test result on the environment the test reads,
	// and the harness reads COHERE_DOCS_CAPTURE, which names a fresh temporary directory every run. So each
	// package that records misses the cache and reruns, and a package that never reads the variable records
	// nothing and may stay cached.
	captured := filepath.Join(scratch, "captured")
	test := exec.Command("go", "test", capturedPackages)
	test.Env = append(os.Environ(), capture.Variable+"="+captured)
	test.Stdout, test.Stderr = os.Stderr, os.Stderr
	if err := test.Run(); err != nil {
		return inputs, fmt.Errorf("the lint tests failed under capture, so their cases are not all asserted: %w", err)
	}
	records, err := docsdata.ReadCaptures(captured)
	if err != nil {
		return inputs, err
	}
	var withOptions int
	inputs.Examples, withOptions = docsdata.PickExamples(records)
	fmt.Printf("captured %d records; %d distinct cases ran with options, counted for message ids and never shown\n", len(records), withOptions)
	return inputs, nil
}

// binaryHelp builds the cohere binary and returns what its flag sets print: the top level's, and each
// verb's.
func binaryHelp(scratch string) ([]byte, map[string][]byte, error) {
	binary := filepath.Join(scratch, "cohere")
	build := exec.Command("go", "build", "-o", binary, "./command/cohere")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		return nil, nil, fmt.Errorf("building ./command/cohere for its help: %w", err)
	}

	help, err := helpOutput(binary, "-help")
	if err != nil {
		return nil, nil, err
	}
	verbHelp := map[string][]byte{}
	for _, verb := range verbs {
		if verbHelp[verb], err = helpOutput(binary, verb, "-help"); err != nil {
			return nil, nil, err
		}
	}
	return help, verbHelp, nil
}

// helpOutput runs the binary with arguments ending in -help and returns the help it printed, without the
// flag package's `Usage of <path>:` line, which names the temporary binary, or the `cohere: flag: help
// requested` a verb's flag set adds when it returns the request as an error.
func helpOutput(binary string, arguments ...string) ([]byte, error) {
	command := exec.Command(binary, arguments...)
	command.Dir = filepath.Dir(binary)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, err
		}
	}
	var kept []string
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, "Usage of ") || line == "cohere: flag: help requested" {
			continue
		}
		kept = append(kept, line)
	}
	return []byte(strings.Join(kept, "\n")), nil
}

// removeScratch removes the temporary directory's files one by one, by name, and then the directories.
func removeScratch(scratch string) {
	var directories []string
	_ = filepath.WalkDir(scratch, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			directories = append(directories, path)
			return nil
		}
		return os.Remove(path)
	})
	for index := len(directories) - 1; index >= 0; index-- {
		_ = os.Remove(directories[index])
	}
}
