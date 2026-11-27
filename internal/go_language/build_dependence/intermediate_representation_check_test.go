package build_dependence

// The IR-level check (#ss4t5g6): the unit-level check proves an independent unit's type information is the same in
// both builds, but the rules that read our intermediate representation read its lowering, which can differ where type
// information doesn't: a composite literal zeroes every field of its struct, a call's lowering hangs on whether the
// callee returns, and wrappers, conversions and ranges read a type's layout or method set. This builds each package of
// each build as production builds it, NewPackage with ctrlflow's no-return predicate over that build's own packages,
// lifted, with the expression map on, and compares every function of every unit block 1 calls independent with its
// counterpart in the other build, found by its file and offset, instruction by instruction. Block 4's independent
// units are block 1's less those block 2's facts judgment sends to be judged again; for the IR, whose lowering after a
// call reads the callee's no-return, that judgment is ctrlflow's no-return compared between the builds
// (noReturnJudgment), and a difference it explains is reported apart from one it doesn't.

import (
	"fmt"
	"maps"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_language/rules/build_intermediate_representation"
)

// controlFlows are ctrlflow's graphs for each package of a build it ran on to the end, by package ID: the no-return
// predicate production hands NewPackage is each package's own.
type controlFlows map[string]*ctrlflow.CFGs

// controlFlowsOf runs ctrlflow over every package of loaded through cohere-go's own checker, every package a root so
// none of them is let go of, as a run's checker runs it on each package and its dependencies: the facts each package
// exports reach its importers' passes, so each predicate answers for its callees as production's does.
func controlFlowsOf(test *testing.T, loaded loadedBuild) controlFlows {
	test.Helper()
	every := slices.SortedFunc(maps.Values(loaded), func(first *packages.Package, second *packages.Package) int {
		return strings.Compare(first.ID, second.ID)
	})
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{ctrlflow.Analyzer}, every, &checker.Options{})
	if analyzeError != nil {
		test.Fatal(analyzeError)
	}
	flows := controlFlows{}
	for _, root := range graph.Roots {
		if graphs, isGraphs := root.Result.(*ctrlflow.CFGs); isGraphs && root.Err == nil {
			flows[root.Package.ID] = graphs
		}
	}
	return flows
}

// fixtureFlows are the fixtures' ctrlflow runs, each run once for the package's tests however many ask for it.
var (
	fixtureFlowsLock sync.Mutex
	fixtureFlows     = map[loadKey]func() controlFlows{}
)

// fixtureControlFlows is controlFlowsOf the fixture's load under build, run once.
func fixtureControlFlows(test *testing.T, fixture string, build fixtureBuild) controlFlows {
	test.Helper()
	loaded := loadFixture(test, fixture, build)
	fixtureFlowsLock.Lock()
	flows, isRunning := fixtureFlows[loadKey{fixture: fixture, build: build.name}]
	if !isRunning {
		flows = sync.OnceValue(func() controlFlows { return controlFlowsOf(test, loaded) })
		fixtureFlows[loadKey{fixture: fixture, build: build.name}] = flows
	}
	fixtureFlowsLock.Unlock()
	return flows()
}

// buildPackage is loaded in our intermediate representation, built whole, as the build_intermediate_representation
// analyzer builds it with no resolver to share one: NewPackage with the package's own ctrlflow predicate, the
// expression map on, as a driver records it where a reader of the map runs, then Build, which lifts. A build that
// panics comes back as the panic and its stack.
func buildPackage(loaded *packages.Package, graphs *ctrlflow.CFGs) (built *intermediate_representation.Package,
	panicked string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			built, panicked = nil, fmt.Sprintf("%v\n%s", recovered, debug.Stack())
		}
	}()
	built = build_intermediate_representation.NewPackage(loaded.Fset, loaded.Types, loaded.Syntax,
		loaded.TypesInfo, graphs.NoReturn, true)
	built.Build()
	return built, ""
}

// compareBuilds compares every package host and build both hold that loaded without errors and that ctrlflow ran
// on to the end in each, a few at once, so only those packages' two lowerings are live together. Each finding says
// whether judgedAgain, the no-return facts judgment, explains it. dropFromBuild is comparePackage's, nil but in the
// test that proves an unpaired function goes red.
func compareBuilds(host loadedBuild, build loadedBuild, hostFlows controlFlows, buildFlows controlFlows,
	dependence *Dependence, judgedAgain []bool,
	dropFromBuild func(*intermediate_representation.Function) bool) loweringTally {
	judge := loweringJudge{dependence: dependence, units: dependence.Units(), judgedAgain: judgedAgain}
	var tally loweringTally
	var tallyLock sync.Mutex
	work := make(chan string)
	var group sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		group.Go(func() {
			for identifier := range work {
				compared := comparePackage(host[identifier], build[identifier], hostFlows[identifier],
					buildFlows[identifier], judge, dropFromBuild)
				tallyLock.Lock()
				tally.merge(compared)
				tallyLock.Unlock()
			}
		})
	}
	for _, identifier := range slices.Sorted(maps.Keys(host)) {
		hosted, built := host[identifier], build[identifier]
		switch {
		case built == nil:
			continue
		case len(hosted.Errors) > 0 || len(built.Errors) > 0 || hosted.TypesInfo == nil || built.TypesInfo == nil ||
			hostFlows[identifier] == nil || buildFlows[identifier] == nil:
			tally.skipped = append(tally.skipped, identifier)
			continue
		}
		work <- identifier
	}
	close(work)
	group.Wait()
	slices.SortFunc(tally.findings, func(first loweringFinding, second loweringFinding) int {
		return strings.Compare(first.unit+first.difference.function, second.unit+second.difference.function)
	})
	slices.SortFunc(tally.packageLevelDiffer, func(first loweringFinding, second loweringFinding) int {
		return strings.Compare(first.unit, second.unit)
	})
	slices.Sort(tally.unpairedIndependent)
	slices.Sort(tally.unpairedOther)
	return tally
}

// loweringFailures are what fails a comparison as a difference does: each difference in a function of a unit block
// 1 calls independent that no callee's no-return explains, and each host function or initializer segment left without
// a counterpart that neither its lack of syntax nor a file only the host compiles accounts for, since the gate would
// pass it uncompared.
func loweringFailures(tally loweringTally) []string {
	var failures []string
	for _, finding := range tally.findings {
		if !finding.judgedAgain {
			failures = append(failures, "independent and lowers differently: "+describeFinding(finding))
		}
	}
	for _, unpaired := range tally.unpairedIndependent {
		failures = append(failures, "in an independent unit and left without a counterpart: "+unpaired)
	}
	for _, unpaired := range tally.unpairedOther {
		failures = append(failures, "left without a counterpart, in a file both builds compile: "+unpaired)
	}
	return failures
}

// describeFinding renders a finding for the log.
func describeFinding(finding loweringFinding) string {
	callee := ""
	if finding.calleeDependent {
		callee = ", a unit it calls is dependent"
	}
	if finding.judgedAgain {
		callee += ", judged again for a callee's no-return"
	}
	return fmt.Sprintf("%s, %s%s: %s\n\thost:  %s\n\tbuild: %s", finding.unit, finding.difference.function, callee,
		finding.difference.where, finding.difference.host, finding.difference.build)
}

// The IR-level check over the fixtures: for every fixture and build, every function of every unit block 4 takes as
// independent, block 1's independent units less those the no-return facts judgment sends to be judged again, lowers
// the same in the host and the build, and on linux some dependent function of the layout fixture lowers differently,
// which shows the comparison can see a difference. The methods fixture's instances of the generic Pair, its method
// values and the methods it promotes through embedded interfaces are compared as every function is, synthetic ones
// reached among them. The lowering fixture pins both views on linux: Guard, independent
// by block 1, lowers differently, since Halt, which it calls, never returns on darwin and returns on linux, and the
// facts judgment sends Guard to be judged again, so it is no difference among block 4's independent units.
func TestEveryIndependentFunctionLowersTheSameInBothBuilds(test *testing.T) {
	test.Parallel()

	for _, fixture := range []string{"layout", "methods", "lowering"} {
		for _, build := range []fixtureBuild{linuxBuild, taggedBuild} {
			test.Run(fixture+" "+build.name, func(test *testing.T) {
				test.Parallel()

				judged := judge(test, fixture, build)
				hostFlows := fixtureControlFlows(test, fixture, hostBuild)
				buildFlows := fixtureControlFlows(test, fixture, build)
				noReturn := judgeNoReturn(judged.host, judged.build, hostFlows, buildFlows, judged.dependence)
				tally := compareBuilds(judged.host, judged.build, hostFlows, buildFlows, judged.dependence,
					noReturn.judgedAgain, nil)
				test.Logf("%d packages compared; %d independent functions and initializer segments (%d of them "+
					"segments), %d judged again for a callee's no-return, %d instructions, %d synthetic functions "+
					"reached; without a counterpart: %d built from no syntax, %d in files only the host compiles; %d "+
					"package-level initializer segments; %d of %d dependent functions lower differently; no-return "+
					"differs for %q", tally.packages, tally.independent, tally.segments, tally.judgedAgain,
					tally.instructions, tally.synthetic, tally.unpairedNoSyntax, tally.unpairedHostOnly,
					tally.packageLevel, tally.dependentDiffer, tally.dependent, noReturn.differingFunctions)
				for _, failure := range loweringFailures(tally) {
					test.Error(failure)
				}
				var explained []string
				for _, finding := range tally.findings {
					if finding.judgedAgain {
						test.Logf("independent by block 1 and lowers differently, judged again: %s",
							describeFinding(finding))
						explained = append(explained, finding.difference.function)
					}
				}
				var wantExplained []string
				if fixture == "lowering" && build.name == linuxBuild.name {
					wantExplained = []string{"example.com/lowering.Guard"}
				}
				if !slices.Equal(explained, wantExplained) {
					test.Errorf("want exactly %q independent by block 1, lowering differently and judged again for "+
						"a callee's no-return, got %q", wantExplained, explained)
				}
				for _, finding := range tally.packageLevelDiffer {
					test.Errorf("a package initializer's segment of no unit lowers differently: %s",
						describeFinding(finding))
				}
				for _, unknown := range slices.Sorted(maps.Keys(tally.unknownFields)) {
					test.Errorf("an instruction field of a kind the comparison does not render: %s", unknown)
				}
				if len(tally.skipped) > 0 {
					test.Errorf("skipped packages: %q", tally.skipped)
				}
				if tally.independent == 0 {
					test.Error("compared no independent function")
				}
				if fixture == "layout" && build.name == linuxBuild.name && tally.dependentDiffer == 0 {
					test.Error("no dependent function lowers differently, so the comparison sees nothing")
				}
			})
		}
	}
}

// A host function left without a counterpart in a file both builds compile fails as a difference does, since the
// gate would pass it uncompared: on linux, with Named dropped from the build's functions, Named's unit is independent
// and the comparison names it, and with NewRecord dropped, NewRecord's unit is dependent and it names it apart.
func TestAnUnpairedFunctionInASharedFileFails(test *testing.T) {
	test.Parallel()

	judged := judge(test, "lowering", linuxBuild)
	hostFlows := fixtureControlFlows(test, "lowering", hostBuild)
	buildFlows := fixtureControlFlows(test, "lowering", linuxBuild)
	noReturn := judgeNoReturn(judged.host, judged.build, hostFlows, buildFlows, judged.dependence)
	tally := compareBuilds(judged.host, judged.build, hostFlows, buildFlows, judged.dependence, noReturn.judgedAgain,
		func(function *intermediate_representation.Function) bool {
			return function.Name() == "Named" || function.Name() == "NewRecord"
		})
	failures := loweringFailures(tally)
	for _, want := range []struct {
		unpaired []string
		name     string
	}{
		{tally.unpairedIndependent, "example.com/lowering.Named"},
		{tally.unpairedOther, "example.com/lowering.NewRecord"},
	} {
		if len(want.unpaired) != 1 || !strings.Contains(want.unpaired[0], want.name+" at ") {
			test.Errorf("want %s alone left without a counterpart in its class, got %q", want.name, want.unpaired)
		}
		if !slices.ContainsFunc(failures, func(failure string) bool { return strings.Contains(failure, want.name) }) {
			test.Errorf("want %s among the failures, got %q", want.name, failures)
		}
	}
}

// On linux, the lowering fixture's Slots is 2 where the host's is 4, stop returns where the host's never does, and
// Speaker gains Loud. Block 1 calls a unit dependent by its kinds of use: a composite literal of Record touches its
// underlying, any use of Speaker reads its name, whose method set moved, and a call of Halt reads only Halt's name,
// which no build changes, though Halt's body is dependent.
func TestLoweringVerdictsOnLinux(test *testing.T) {
	test.Parallel()

	judged := judge(test, "lowering", linuxBuild)
	expectVerdicts(test, verdicts(test, judged.host, judged.dependence), map[string]bool{
		"type Record":          true,
		"func NewRecord":       true,
		"func PointRecord":     true,
		"func Named":           false,
		"func Halt":            true,
		"func Guard":           false,
		"type Speaker":         true,
		"method Speaker.Quiet": true,
		"func Bound":           true,
		"func Thunk":           true,
		"func Converted":       true,
		"type Loudness":        false,
		"func IsLoud":          false,
		"value Slots":          true,
		"func stop":            true,
		"value Labels":         false,
		"value Greet":          false,
		"value platformLabel":  true,
		"value _":              false,
	})
}
