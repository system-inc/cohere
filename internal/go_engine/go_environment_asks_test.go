package go_engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// A whole run under `--other-builds` over a module whose census needs one extra build asks go env once per build,
// twice: the census's answer serves the host's findings cache and its load, the lister and the kept listing among
// it, and the extra build's one answer serves its own cache and load alike (#4rpt1jh). A second run, over the tree
// and the findings cache the first left, asks the same.
func TestARunAsksGoEnvironmentOncePerBuild(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	// Without its windows file the module needs one extra build beside the host's, the cohere_extra tag's.
	windowsFile := filepath.Join(root, "internal", "separator", "separator_windows.go")
	if removeError := os.Remove(windowsFile); removeError != nil {
		test.Fatal(removeError)
	}
	asks, stop := load.WatchGoEnvironmentAsks(root)
	defer stop()
	for _, run := range []string{"first", "second"} {
		asks.Store(0)
		engine, _ := runOverWith(test, root, exitClean, "--other-builds")
		if len(engine.buildRuns) != 2 {
			test.Fatalf("the %s run checked %d builds, want the host's and one extra", run, len(engine.buildRuns))
		}
		if count := asks.Load(); count != 2 {
			test.Errorf("the %s run asked go env %d times, want once per build, 2", run, count)
		}
	}
}

// The findings cache's key reads `GOROOT` and every one of buildNames from go env's one answer, so each is among what
// it asks: one left out would read as empty in every key, and never miss.
func TestTheGoEnvironmentAnswersEveryNameTheCacheReads(test *testing.T) {
	test.Parallel()
	values := goEnvironmentIn(test, copyFixture(test, filepath.Join("testdata", "other-builds")), nil)
	for _, name := range append([]string{"GOROOT"}, buildNames...) {
		if _, answered := values[name]; !answered {
			test.Errorf("go env's answer holds no %s, which the findings cache's key reads", name)
		}
	}
}
