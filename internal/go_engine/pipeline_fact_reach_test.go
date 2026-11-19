package go_engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Under the body reach census, with `--other-builds --build-dependence`, the host's lint hands back what its run held
// of its facts for the facts' reach (#aswfm7a), and the run makes of it, as the builds start, the body-reach fixture's
// one package's side, from its record and its answers, then lets the capture go. A run without the census asks for
// none and makes no side, its findings the same as the census's.
func TestTheHostsSidesForTheFactsReachAreMadeUnderTheBodyReachCensus(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach"))
	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	on, onStream := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = censusFile
	}, "--no-cache", "--no-fix", "--other-builds", "--build-dependence")
	off, offStream := runOver(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings,
		"--no-fix", "--other-builds", "--build-dependence")

	if len(on.buildRuns) != 2 || len(off.buildRuns) != 2 {
		test.Fatalf("want the host's build and windows's in both runs, got %d and %d runs", len(on.buildRuns),
			len(off.buildRuns))
	}
	if on.factReachSides == nil {
		test.Fatal("under the census the run made no sides for the facts' reach")
	}
	hosted := on.factReachSides.packages["example.com/reach"]
	// Seven bodies in reach.go, and platformName in the host's own reach_other.go.
	if len(on.factReachSides.packages) != 1 || hosted == nil || len(hosted.Places()) != 8 {
		test.Errorf("under the census the run made the sides %v, want example.com/reach's alone, with its 8 bodies",
			on.factReachSides.packages)
	}
	if on.buildRuns[0].result.FactReach != nil {
		test.Error("the host's capture outlived the sides made of it")
	}
	written, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	line := "fact reach: packages given a side 1; none, uncaptured 0, unrecorded 0, order unknown 0"
	if !slices.Contains(strings.Split(string(written), "\n"), line) {
		test.Errorf("the census file has no line %q:\n%s", line, written)
	}
	if off.buildRuns[0].result.FactReach != nil || off.factReachSides != nil {
		test.Error("a run without the census captured the host's facts")
	}
	if !slices.Equal(findingLines(normalize(onStream, root)), findingLines(offStream)) {
		test.Errorf("the findings with the census are\n%q\nwant those without it\n%q",
			findingLines(normalize(onStream, root)), findingLines(offStream))
	}
}

// On a warm run the host restores the body-reach fixture's one package from the findings store, so its run computed
// nothing there and holds no side of it: the census counts it uncaptured, and every build lints it whole (#aswfm7a).
func TestAWarmRunGivesARestoredPackageNoSideForTheFactsReach(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach"))
	runOverConfigured(test, root, exitFindings, nil, "--no-fix", "--other-builds", "--build-dependence")
	warm, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
	}, "--no-fix", "--other-builds", "--build-dependence")

	if warm.factReachSides == nil {
		test.Fatal("the warm run under the census made no sides for the facts' reach")
	}
	if len(warm.factReachSides.packages) != 0 || warm.factReachSides.uncaptured != 1 {
		test.Errorf("the warm run made the sides %v with %d uncaptured, want none, and example.com/reach uncaptured",
			warm.factReachSides.packages, warm.factReachSides.uncaptured)
	}
}
