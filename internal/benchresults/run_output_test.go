package benchresults

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The reader reads real runs. Each file in testdata/run-output is a `cohere --json` run's whole log,
// stdout and stderr together as quiet_machine.sh keeps it, captured from cohere at 91cf8e39 on a project of
// three files: a cold run under --no-cache, a warm run that took every file from the cache, an edit run
// after a comment was added to one file, and a run with a debugger statement and an unused constant in a
// fourth, so a count of zero has been seen to read nonzero (#zrgrk14).
func TestRunOutputReadsRealRuns(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]struct {
		cache       string
		hasFindings bool
	}{
		"cold.txt":     {cache: "off"},
		"warm.txt":     {cache: "files 3/3"},
		"edit.txt":     {cache: "files "},
		"findings.txt": {cache: "", hasFindings: true},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			output, readError := os.ReadFile(filepath.Join("testdata", "run-output", name))
			if readError != nil {
				test.Fatal(readError)
			}
			run, parseRunOutputError := ParseRunOutput(output)
			if parseRunOutputError != nil {
				test.Fatal(parseRunOutputError)
			}
			if run.EngineSeconds <= 0 {
				test.Errorf("engine seconds read %g", run.EngineSeconds)
			}
			if !strings.HasPrefix(run.Cache, want.cache) {
				test.Errorf("cache read %q, want %q", run.Cache, want.cache)
			}
			if want.cache == "files " {
				edited := Run{Mode: ModeEdit, Cache: run.Cache}
				if cacheFitsModeError := cacheFitsMode(edited); cacheFitsModeError != nil {
					test.Errorf("a real edit run's cache does not fit an edit: %v", cacheFitsModeError)
				}
			}
			if (run.Findings > 0) != want.hasFindings {
				test.Errorf("findings read %d", run.Findings)
			}
		})
	}
}

// A log the reader cannot read is refused, not read as zeroes: the human footer alone, which is what the
// first record's instrument read, and a summary of a contract version it does not know.
func TestRunOutputRefusesWhatItCannotRead(test *testing.T) {
	test.Parallel()
	for name, output := range map[string]string{
		"the footer alone":         "✓ 💎 0.1s (430 rules • 3,976 cached)\n",
		"another schema":           `{"kind":"summary","schemaVersion":2,"seconds":0.1}` + "\n",
		"findings with no summary": `{"kind":"finding","path":"/a.ts","line":1,"column":1}` + "\n",
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			if run, parseRunOutputError := ParseRunOutput([]byte(output)); parseRunOutputError == nil {
				test.Fatalf("read %+v", run)
			}
		})
	}
}
