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
