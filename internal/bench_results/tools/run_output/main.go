// Command run_output prints what one measured run said about itself, read from its `--json` output
// (bench_results.ParseRunOutput), as quiet_machine.sh's columns: engine seconds, findings, cache use, and the builds
// its Go project checked, joined by `; ` (a build's own label holds spaces and commas), tab-separated. quiet_machine.sh
// builds it once and runs it after each measured run, outside the timing:
//
//	run_output <log>
//
// It exits 1 when the log holds no summary it can read, or one of its projects did less than a developer's
// run would (an engine that did not run, a gap), so the benchmark stops rather than recording a column it
// could not fill or a time for work that was never done.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/system-inc/cohere/internal/bench_results"
)

func main() {
	if len(os.Args) != 2 {
		// Ignored: the exit code below says the run failed even when standard error cannot.
		fmt.Fprintln(os.Stderr, "usage: run_output <log>")
		os.Exit(2)
	}
	output, readError := os.ReadFile(os.Args[1])
	if readError != nil {
		// Ignored: the exit code below says the run failed even when standard error cannot.
		fmt.Fprintf(os.Stderr, "run_output: %v\n", readError)
		os.Exit(1)
	}
	run, parseRunOutputError := bench_results.ParseRunOutput(output)
	if parseRunOutputError != nil {
		// Ignored: the exit code below says the run failed even when standard error cannot.
		fmt.Fprintf(os.Stderr, "run_output: %s: %v\n", os.Args[1], parseRunOutputError)
		os.Exit(1)
	}
	columns := fmt.Sprintf("%.3f\t%d\t%s\t%s", run.EngineSeconds, run.Findings, run.Cache,
		strings.Join(run.Builds, bench_results.BuildsSeparator))
	if _, printError := fmt.Println(columns); printError != nil {
		// Ignored: the exit code below says the columns never arrived even when standard error cannot.
		fmt.Fprintf(os.Stderr, "run_output: printing the columns: %v\n", printError)
		os.Exit(1)
	}
}
