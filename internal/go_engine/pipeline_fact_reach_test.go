package go_engine

import (
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// Under the body reach census, with `--build-dependence`, the host's lint hands back what its run held of its facts
// for the facts' reach (#aswfm7a), and the run makes of it, as the builds start, the fact-reach-sides fixture's one
// package's side, from its record and its answers, then lets the capture go. A run without the census asks for none
// and makes no side, its findings the same as the census's.
func TestTheHostsSidesForTheFactsReachAreMadeUnderTheBodyReachCensus(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "fact-reach-sides"))
	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	on, onStream := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = censusFile
	}, "--no-cache", "--no-fix", "--build-dependence")
	off, offStream := runOver(test, copyFixture(test, filepath.Join("testdata", "fact-reach-sides")), exitFindings,
		"--no-fix", "--build-dependence")

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
	line := "fact reach: packages given a side 1; none, uncaptured 0, answered from the store 0, unrecorded 0, order " +
		"unknown 0"
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

// Under the body reach census windows's build hands the passes on the fact-reach-sides fixture's one package the host's
// side of it for the facts' reach, with the two bodies its census leaves unreached, Gadget.Spin and Joined, standing,
// and so lints the package in part (#aswfm7a). The host's run hands none, and nor does a build without the census.
func TestEachBuildHandsItsPassesTheHostsSideWithItsUnreachedBodiesStanding(test *testing.T) {
	test.Parallel()

	on, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "fact-reach-sides")), exitFindings,
		func(engine *pipeline) { engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt") }, "--no-cache",
		"--no-fix", "--build-dependence")
	off, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "fact-reach-sides")), exitFindings, "--no-fix",
		"--build-dependence")

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

// Under the body reach census windows's build settles the standing bodies of the fact-reach-sides fixture's one package
// by standing_facts, which reads the side its passes were handed, and the census writes what that took (#aswfm7a): the
// side used, its 8 functions' no-return facts compared, platformName's named, since windows declares its own in a file
// of its own, and nothing taken out, since PlatformName, platformName's one user, is reached already. No fact kind
// answered about the package, so nothing is asked. A build without the census settles nothing.
func TestEachBuildSettlesItsStandingBodiesAndTheCensusCountsThem(test *testing.T) {
	test.Parallel()

	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	on, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "fact-reach-sides")), exitFindings,
		func(engine *pipeline) { engine.bodyReachCensus = censusFile }, "--no-cache", "--no-fix", "--build-dependence")
	off, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "fact-reach-sides")), exitFindings, "--no-fix",
		"--build-dependence")

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

// On a warm run the host restores the fact-reach-sides fixture's one package from the findings store, so its run
// computed nothing there and holds no side of it: the census counts it uncaptured, and every build lints it whole
// (#aswfm7a).
func TestAWarmRunGivesARestoredPackageNoSideForTheFactsReach(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "fact-reach-sides"))
	runOverConfigured(test, root, exitFindings, nil, "--no-fix", "--build-dependence")
	warm, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
	}, "--no-fix", "--build-dependence")

	if warm.factReachSides == nil {
		test.Fatal("the warm run under the census made no sides for the facts' reach")
	}
	if len(warm.factReachSides.packages) != 0 || warm.factReachSides.uncaptured != 1 {
		test.Errorf("the warm run made the sides %v with %d uncaptured, want none, and example.com/reach uncaptured",
			warm.factReachSides.packages, warm.factReachSides.uncaptured)
	}
}

// Under the body reach census and the body differential, the differential's second leg checks what the facts' reach
// left standing on windows in the standing-facts fixture (#adq5ahd). Wrapped calls value, which each platform's file
// declares, so the reach lints it; its nil-ness differs, so Outer, its user, leaves, and Outer's differs in turn, so
// OuterFailed leaves too. What stands, Stable, StableFailed, stableError.Error, Quiet and Hidden, has every answer the
// build's own run holds about it the host's, or one only the build holds that no standing body asked: Stable's
// nil-ness, asked where StableFailed compares its error with nil, the five no-return facts and pushed facts; Quiet's
// nil-ness, which only windows's quietFailed asks, a body the build lints (#5vbs1np, must 5); and Hidden's, which
// windows's judgment of viaHidden keeps and no rule asks. Nothing differs, so the leg has no failure, and the census
// counts the two answers only the build holds apart.
func TestTheSecondLegComparesWhatTheFactsReachLeftStanding(test *testing.T) {
	test.Parallel()

	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	on, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "standing-facts")), exitFindings,
		func(engine *pipeline) {
			engine.bodyReachCensus = censusFile
			engine.bodyDifferential = true
		}, "--no-cache", "--no-fix", "--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].bodyDifferential == nil {
		test.Fatalf("want windows's body differential, got %d runs", len(on.buildRuns))
	}
	differential := on.buildRuns[1].bodyDifferential
	wantCompared := map[string]int{"fact_deprecated": 5, standingNoReturn: 5, "printf": 5, "return_nil_ness": 3}
	if !maps.Equal(differential.standingCompared, wantCompared) || len(differential.standingFailed) > 0 ||
		len(differential.failures) > 0 {
		test.Errorf("windows's second leg compared %v and failed %v, with the failures\n%s\nwant %v compared and "+
			"none failed", differential.standingCompared, differential.standingFailed,
			strings.Join(differential.failures, "\n"), wantCompared)
	}
	written, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	for _, line := range []string{
		"  fact reach: sides 1, settled 1, crashed 0",
		"    answers asked 3, differing return_nil_ness 2, unknown return_nil_ness 1; bodies taken out 2",
		"  standing facts: compared fact_deprecated 5, no-return 5, printf 5, return_nil_ness 3; failures none",
		"  standing facts only the build holds: asked only in the build, by reached bodies 1; held only in the " +
			"build, asked by no rule 1",
	} {
		if !slices.Contains(strings.Split(string(written), "\n"), line) {
			test.Errorf("the census file has no line %q:\n%s", line, written)
		}
	}
}

// The second leg fails where what stands differs, shown by handing it a host's side that says otherwise of Stable
// (#5vbs1np, must 4): the build's answer about Stable's nil-ness is its own, so a host's answer of other bytes is a
// failure on a line of its own, counted under the kind, and in the census, and so is none at all, since StableFailed,
// the body whose rule asks it, stands (must 5): its findings would rest on an answer the host never had. Nothing else
// moves: Stable stands, so the facts' reach never asks the answer, and the leg alone reads it. A deprecation the host
// held and the build does not is the facts' reach's own to compare, before the leg reads anything
// (TestAHostsDeprecationTheBuildLacksTakesTheFunctionAndItsUserOut).
func TestTheSecondLegFailsWhereAStandingAnswerIsNotTheHosts(test *testing.T) {
	test.Parallel()

	// stableAsking is the key of the host's answer about Stable's nil-ness in hosted, and the answer.
	stableAsking := func(hosted *factReachPackage) (string, fact_reach.Asking) {
		for key, asking := range hosted.askings {
			if asking.Kind == "return_nil_ness" && asking.Function.Path == "Stable" && asking.Parameter < 0 {
				return key, asking
			}
		}
		return "", fact_reach.Asking{}
	}
	for _, each := range []struct {
		name string
		kind string
		// alter changes the host's side of Stable, and is the line's end the leg must fail with.
		alter func(hosted *factReachPackage) string
	}{
		{"differs", "return_nil_ness", func(hosted *factReachPackage) string {
			key, asking := stableAsking(hosted)
			built := string(asking.Encoded)
			asking.Encoded = []byte("otherwise")
			hosted.askings[key] = asking
			return " Stable return_nil_ness: host otherwise, build " + built
		}},
		{"host none", "return_nil_ness", func(hosted *factReachPackage) string {
			key, asking := stableAsking(hosted)
			delete(hosted.askings, key)
			return " Stable return_nil_ness: host none, build " + string(asking.Encoded)
		}},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			censusFile := filepath.Join(test.TempDir(), "reach.txt")
			var wantLine string
			on, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "standing-facts")),
				exitFindings, func(engine *pipeline) {
					engine.bodyReachCensus = censusFile
					engine.bodyDifferential = true
					// Windows's is the one extra build, and its census comes before its passes read the side.
					engine.bodyDifferentialCensus = func(census *buildBodyReach) *buildBodyReach {
						wantLine = each.alter(engine.factReachSides.packages["example.com/standing"])
						return census
					}
				}, "--no-cache", "--no-fix", "--build-dependence")
			if len(on.buildRuns) != 2 || on.buildRuns[1].bodyDifferential == nil {
				test.Fatalf("want windows's body differential, got %d runs", len(on.buildRuns))
			}
			differential := on.buildRuns[1].bodyDifferential
			if !maps.Equal(differential.standingFailed, map[string]int{each.kind: 1}) ||
				!slices.ContainsFunc(differential.failures, func(line string) bool {
					return strings.HasSuffix(line, wantLine)
				}) {
				test.Errorf("windows's second leg failed %v, with the failures\n%s\nwant one %s, ending %q",
					differential.standingFailed, strings.Join(differential.failures, "\n"), each.kind, wantLine)
			}
			written, readError := os.ReadFile(censusFile)
			if readError != nil {
				test.Fatal(readError)
			}
			line := "  standing facts: compared fact_deprecated 5, no-return 5, printf 5, return_nil_ness 3; " +
				"failures " + each.kind + " 1"
			if !slices.Contains(strings.Split(string(written), "\n"), line) {
				test.Errorf("the census file has no line %q:\n%s", line, written)
			}
		})
	}
}

// A deprecation the host held and the build does not is compared by the facts' reach (#7v2bzvd), shown by handing
// windows's build a host's side that says Stable is deprecated: Stable is named for "deprecation", and both
// StableFailed, which calls it, and Stable's own body, whose findings read whether Stable itself is deprecated, leave
// the standing set for it, and for nothing else. Nothing that stands differs, so the second leg fails nothing.
func TestAHostsDeprecationTheBuildLacksTakesTheFunctionAndItsUserOut(test *testing.T) {
	test.Parallel()

	on, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "standing-facts")), exitFindings,
		func(engine *pipeline) {
			engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
			engine.bodyDifferential = true
			// Windows's is the one extra build, and its census comes before its passes read the side.
			engine.bodyDifferentialCensus = func(census *buildBodyReach) *buildBodyReach {
				hosted := engine.factReachSides.packages["example.com/standing"]
				for index, function := range hosted.functions {
					if function.Identity.Path == "Stable" {
						pushed := maps.Clone(function.Pushed)
						pushed["fact_deprecated"] = "Deprecated: otherwise"
						hosted.functions[index].Pushed = pushed
					}
				}
				return census
			}
		}, "--no-cache", "--no-fix", "--build-dependence")
	if len(on.buildRuns) != 2 || on.buildRuns[1].bodyDifferential == nil {
		test.Fatalf("want windows's body differential, got %d runs", len(on.buildRuns))
	}
	settled, isSettled := on.buildRuns[1].result.FactReachSettled["example.com/standing"].(*standing_facts.Result)
	if !isSettled || settled.Settlement == nil {
		test.Fatalf("windows settled example.com/standing as %+v, want it settled in part", settled)
	}
	names := map[fact_reach.Place]string{}
	for _, body := range on.buildRuns[1].bodyReach.bodies {
		names[fact_reach.Place{File: body.file, Offset: body.offset}] = body.name
	}
	removed := map[string]fact_reach.Reason{}
	for _, removal := range settled.Settlement.Removed {
		if slices.Contains(strings.Split(string(removal.Object), "\x00"), "Stable") {
			removed[names[removal.Place]] = removal.Reason
		}
	}
	if want := map[string]fact_reach.Reason{"Stable": "deprecation", "StableFailed": "deprecation"}; !maps.Equal(
		removed, want) {
		test.Errorf("Stable took out %v, want %v", removed, want)
	}
	differential := on.buildRuns[1].bodyDifferential
	if len(differential.standingFailed) > 0 {
		test.Errorf("windows's second leg failed %v, with the failures\n%s\nwant nothing standing that differs",
			differential.standingFailed, strings.Join(differential.failures, "\n"))
	}
}

// An asker stands where the facts' reach left its body standing, and one no place names, as a question asked with no
// asker is recorded (round 2's N3), stands too, the side that fails; a body of a package with no settlement, which the
// build lints whole, never does.
func TestAnAskerNoPlaceNamesIsTakenAsStanding(test *testing.T) {
	test.Parallel()

	settledTypes := types.NewPackage("example.com/settled", "settled")
	wholeTypes := types.NewPackage("example.com/whole", "whole")
	standing := fact_reach.Place{File: "settled.go", Offset: 40}
	stands := standingAskers(map[string]any{"settled": &standing_facts.Result{Partial: true,
		Settlement: &fact_reach.Settlement{Standing: map[fact_reach.Place]bool{standing: true}}}},
		map[string]*packages.Package{"settled": {Types: settledTypes}, "whole": {Types: wholeTypes}})
	for _, each := range []struct {
		asker fact_resolver.Asker
		want  bool
	}{
		{fact_resolver.Asker{}, true},
		{fact_resolver.Asker{Package: settledTypes}, true},
		{fact_resolver.Asker{Package: settledTypes, File: standing.File, Offset: standing.Offset}, true},
		{fact_resolver.Asker{Package: settledTypes, File: standing.File, Offset: 90}, false},
		{fact_resolver.Asker{Package: wholeTypes, File: "whole.go", Offset: 40}, false},
	} {
		if stood := stands(each.asker); stood != each.want {
			test.Errorf("the asker %+v stands %t, want %t", each.asker, stood, each.want)
		}
	}
}
