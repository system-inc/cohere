package go_engine

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
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

// Under the body reach census windows's build hands the passes on the body-reach fixture's one package the host's side
// of it for the facts' reach, with the two bodies its census leaves unreached, Gadget.Spin and Joined, standing, and
// so lints the package in part (#aswfm7a). The host's run hands none, and nor does a build without the census.
func TestEachBuildHandsItsPassesTheHostsSideWithItsUnreachedBodiesStanding(test *testing.T) {
	test.Parallel()

	on, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings,
		func(engine *pipeline) { engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt") }, "--no-cache",
		"--no-fix", "--other-builds", "--build-dependence")
	off, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings, "--no-fix",
		"--other-builds", "--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].bodyReach == nil {
		test.Fatalf("want the host's build and windows's census, got %d runs", len(on.buildRuns))
	}
	if on.buildRuns[0].factHosts != nil {
		test.Error("the host's run handed its passes a side of its own")
	}
	hosted, isHosted := on.buildRuns[1].factHosts["example.com/reach"]
	if len(on.buildRuns[1].factHosts) != 1 || !isHosted {
		test.Fatalf("windows's build handed the sides %v, want example.com/reach's alone", on.buildRuns[1].factHosts)
	}
	want := map[fact_reach.Place]bool{}
	for _, body := range on.buildRuns[1].bodyReach.bodies {
		if body.name == "Gadget.Spin" || body.name == "Joined" {
			want[fact_reach.Place{File: body.file, Offset: body.offset}] = true
		}
	}
	standing, partial := hosted.Standing()
	if len(want) != 2 || !partial || !maps.Equal(standing, want) {
		test.Errorf("windows's build handed the standing set %v, partial %t, want Gadget.Spin's and Joined's %v, "+
			"partial", standing, partial, want)
	}
	if slices.ContainsFunc(off.buildRuns, func(run buildRun) bool { return run.factHosts != nil }) {
		test.Error("a build without the census handed its passes a side")
	}
}

// Under the body reach census windows's build settles the standing bodies of the body-reach fixture's one package by
// standing_facts, which reads the side its passes were handed, and the census writes what that took (#aswfm7a): the
// side used, its 8 functions' no-return facts compared, platformName's named, since windows declares its own in a file
// of its own, and nothing taken out, since PlatformName, platformName's one user, is reached already. No fact kind
// answered about the package, so nothing is asked. A build without the census settles nothing.
func TestEachBuildSettlesItsStandingBodiesAndTheCensusCountsThem(test *testing.T) {
	test.Parallel()

	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	on, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings,
		func(engine *pipeline) { engine.bodyReachCensus = censusFile }, "--no-cache", "--no-fix", "--other-builds",
		"--build-dependence")
	off, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings, "--no-fix",
		"--other-builds", "--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].factReach == nil {
		test.Fatalf("want windows's census of the facts' reach, got %d runs", len(on.buildRuns))
	}
	census := on.buildRuns[1].factReach
	if census.sides != 1 || census.settled != 1 || census.crashed != 0 || census.functionsCompared == 0 {
		test.Errorf("windows's build settled %+v, want its one side used, settled in part, and no-return facts "+
			"compared", census)
	}
	written, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	for _, line := range []string{
		"  fact reach: sides 1, settled 1, crashed 0",
		"    functions compared 8, named 1; imported asked 0, named 0; order bound named 0",
		"    answers asked 0, differing none, unknown none; bodies taken out 0",
	} {
		if !slices.Contains(strings.Split(string(written), "\n"), line) {
			test.Errorf("the census file has no line %q:\n%s", line, written)
		}
	}
	if slices.ContainsFunc(off.buildRuns, func(run buildRun) bool { return run.factReach != nil }) {
		test.Error("a build without the census settled its standing bodies")
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
