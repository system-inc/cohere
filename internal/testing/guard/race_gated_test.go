package guard

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/testing/concurrency_tests"
)

// raceGatedFile is the file holding the land gate's -race list, raceGated, from this package's directory.
var raceGatedFile = filepath.Join("..", "..", "..", "command", "cohere-dev", "race_gated.go")

// Every package holding a test whose name or file says it runs code at once is on the -race list in
// command/cohere-dev's race_gated.go, so a new concurrency test is caught by the race detector at the gate from its
// first landing (#bnabymg). It walks the whole module, so it lives here, with the guards that read the whole tree by
// design, and command/cohere-dev reruns only for what it reads (#0zdyk51). The list is read from its file, parsed
// (concurrency_tests.RaceGatedDirectories), so this package needs nothing of the command, and command/cohere-dev's
// TestTheParsedRaceListIsTheOneTheGateRuns holds the parse equal to the list the gate runs.
func TestEveryConcurrencyTestIsRaceGated(test *testing.T) {
	test.Parallel()
	listed, listError := concurrency_tests.RaceGatedDirectories(raceGatedFile, nil)
	if listError != nil {
		test.Fatal(listError)
	}
	gated := map[string]bool{}
	for _, directory := range listed {
		gated[directory] = true
	}
	found, walkError := concurrency_tests.Directories(filepath.Join("..", "..", ".."))
	if walkError != nil {
		test.Fatal(walkError)
	}
	if len(found) == 0 {
		test.Fatal("no concurrency test was found in the module, so the list was checked against nothing")
	}
	for directory, why := range found {
		if !gated[directory] {
			test.Errorf("%s holds %s, and is not on the -race list in race_gated.go: add it, with why", directory, why)
		}
	}
}
