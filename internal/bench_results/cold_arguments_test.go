package bench_results

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/needs"
)

// coldArgumentsScript is bench/cold_arguments.zsh, which quiet_machine.sh sources for its cold and idle runs'
// arguments, relative to this package.
var coldArgumentsScript = filepath.Join("..", "..", "bench", "cold_arguments.zsh")

// benchArguments runs bench/cold_arguments.zsh's function named by function on argument, as quiet_machine.sh does,
// and gives the arguments it printed. The script is read here, through os, and its bytes
// handed to zsh, rather than zsh opening it: go test's cache keys a test on the files the test process opens, and
// one only zsh opened would let an edit to the script replay a cached pass.
func benchArguments(test *testing.T, function string, argument string) []string {
	test.Helper()
	zsh := needs.Tool(test, "zsh", "the benchmark is a zsh script")
	script, readError := os.ReadFile(coldArgumentsScript)
	if readError != nil {
		test.Fatal(readError)
	}
	printed, runError := exec.Command(zsh, "-c", string(script)+"\n"+function+" \"$1\"", "zsh", argument).Output()
	if runError != nil {
		test.Fatalf("%s %q: %v", function, argument, runError)
	}
	return strings.Fields(string(printed))
}

// A cold run narrowed to the Go engine checks without fixing, since #pff7xhr's bar is a checking run's seconds
// (#jp0cqkk), and reads no cache; a whole-root cold run reads no cache and fixes, as the baselines it is held to did.
func TestTheGoEnginesColdRunChecksWithoutFixing(test *testing.T) {
	test.Parallel()
	for engine, want := range map[string][]string{
		string(EngineGo): {"--no-cache", "--no-fix"},
		"":               {"--no-cache"},
	} {
		test.Run("engine "+engine, func(test *testing.T) {
			test.Parallel()
			if arguments := benchArguments(test, "cold_arguments", engine); !slices.Equal(arguments, want) {
				test.Errorf("cold_arguments %q printed %q, want %q", engine, arguments, want)
			}
		})
	}
}

// quiet_machine.sh runs under `set -u` and a whole-root run names no engine, so each function, called with no
// argument at all, prints what it prints for an empty one and says nothing on standard error. Until 2026-11-12
// cold_arguments read $1 bare and failed after printing `--no-cache`, so the cold runs were still cold only because
// of the order of its two lines, and every bench log opened with the error.
func TestEachArgumentFunctionTakesNoArgumentUnderNounset(test *testing.T) {
	test.Parallel()
	zsh, lookError := exec.LookPath("zsh")
	if lookError != nil {
		test.Fatalf("the benchmark is a zsh script, and zsh is not on the path: %v", lookError)
	}
	script, readError := os.ReadFile(coldArgumentsScript)
	if readError != nil {
		test.Fatal(readError)
	}
	for function, want := range map[string][]string{
		"cold_arguments":  {"--no-cache"},
		"build_arguments": nil,
	} {
		test.Run(function, func(test *testing.T) {
			test.Parallel()
			command := exec.Command(zsh, "-c", "set -u\n"+string(script)+"\n"+function)
			var errors strings.Builder
			command.Stderr = &errors
			printed, runError := command.Output()
			if runError != nil || errors.Len() > 0 {
				test.Fatalf("%s with no argument under set -u: %v, standard error %q",
					function, runError, errors.String())
			}
			if arguments := strings.Fields(string(printed)); !slices.Equal(arguments, want) {
				test.Errorf("%s with no argument printed %q, want %q", function, arguments, want)
			}
		})
	}
}

// Every run of a bench asked for every build checks every build the Go files name, `--other-builds`, and a bench of
// the host's build alone passes nothing, as every record before #ccqee0e measured.
func TestTheBenchAsksForEveryBuildWhenToldTo(test *testing.T) {
	test.Parallel()
	for builds, want := range map[string][]string{
		"every": {"--other-builds"},
		"host":  nil,
	} {
		test.Run("builds "+builds, func(test *testing.T) {
			test.Parallel()
			if arguments := benchArguments(test, "build_arguments", builds); !slices.Equal(arguments, want) {
				test.Errorf("build_arguments %q printed %q, want %q", builds, arguments, want)
			}
		})
	}
}
