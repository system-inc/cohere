package build_dependence

// The IR-level check (#ss4t5g6): the unit-level check proves an independent unit's type information is the same in
// both builds, but the rules that read our intermediate representation read its lowering, which can differ where type
// information doesn't: a composite literal zeroes every field of its struct, a call's lowering hangs on whether the
// callee returns, and wrappers, conversions and ranges read a type's layout or method set. This builds each package of
// each build as production builds it, NewPackage with ctrlflow's no-return predicate over that build's own packages,
// lifted, with the expression map on, and compares every function of every unit block 1 calls independent with its
// counterpart in the other build, found by its file and offset, instruction by instruction.
//
// The ordinary run compares the fixtures. COHERE_GO_BUILD_DEPENDENCE_IR names a tree whose `./...`, tests included,
// the wide run loads under the host and under each build COHERE_GO_BUILD_DEPENDENCE_IR_BUILDS names.

import (
	"fmt"
	"maps"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// The wide run's variables: the tree it loads, and the builds it compares against the host, by name
// (wideBuilds).
const (
	wideTreeVariable   = "COHERE_GO_BUILD_DEPENDENCE_IR"
	wideBuildsVariable = "COHERE_GO_BUILD_DEPENDENCE_IR_BUILDS"
)

// wideDifferencesListed is how many differences in independent functions the wide run lists, beside their count.
const wideDifferencesListed = 200

// controlFlows are ctrlflow's graphs for each package of a build it ran on to the end, by package ID: the no-return
// predicate production hands NewPackage is each package's own.
type controlFlows map[string]*ctrlflow.CFGs

// controlFlowsOf runs ctrlflow over every package of loaded through the engine's own checker, every package a root so
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
// on to the end in each, a few at once, so only those packages' two lowerings are live together.
func compareBuilds(host loadedBuild, build loadedBuild, hostFlows controlFlows, buildFlows controlFlows,
	dependence *Dependence) loweringTally {
	judge := loweringJudge{dependence: dependence, units: dependence.Units()}
	var tally loweringTally
	var tallyLock sync.Mutex
	work := make(chan string)
	var group sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		group.Go(func() {
			for identifier := range work {
				compared := comparePackage(host[identifier], build[identifier], hostFlows[identifier],
					buildFlows[identifier], judge)
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
	return tally
}

// describeFinding renders a finding for the log.
func describeFinding(finding loweringFinding) string {
	callee := ""
	if finding.calleeDependent {
		callee = ", a unit it calls is dependent"
	}
	return fmt.Sprintf("%s, %s%s: %s\n\thost:  %s\n\tbuild: %s", finding.unit, finding.difference.function, callee,
		finding.difference.where, finding.difference.host, finding.difference.build)
}

// The IR-level check over the fixtures: for every fixture and build, every function of every unit block 1 calls
// independent lowers the same in the host and the build, and on linux some dependent function of the layout fixture
// lowers differently, which shows the comparison can see a difference.
func TestEveryIndependentFunctionLowersTheSameInBothBuilds(test *testing.T) {
	test.Parallel()

	for _, fixture := range []string{"layout", "methods", "lowering"} {
		for _, build := range []fixtureBuild{linuxBuild, taggedBuild} {
			test.Run(fixture+" "+build.name, func(test *testing.T) {
				test.Parallel()

				judged := judge(test, fixture, build)
				tally := compareBuilds(judged.host, judged.build, fixtureControlFlows(test, fixture, hostBuild),
					fixtureControlFlows(test, fixture, build), judged.dependence)
				test.Logf("compared %d independent functions, %d instructions, %d synthetic functions reached; %d of "+
					"%d dependent functions lower differently", tally.independent, tally.instructions, tally.synthetic,
					tally.dependentDiffer, tally.dependent)
				for _, finding := range tally.findings {
					test.Errorf("independent and lowers differently: %s", describeFinding(finding))
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

// wideBuild is a build the wide run compares against the host, by the name COHERE_GO_BUILD_DEPENDENCE_IR_BUILDS
// gives it.
var wideBuilds = map[string]struct {
	goos string
	tags string
}{
	"linux":   {goos: "linux"},
	"windows": {goos: "windows"},
	"freebsd": {goos: "freebsd"},
	"android": {goos: "android"},
	"race":    {goos: "darwin", tags: "race"},
}

// wideEnvironment is the process's environment with the build's `GOOS` and tags on arm64, cgo off, so no file is the
// cgo tool's output whose name changes between builds, and with `-buildvcs=false`, since a worktree whose submodule
// is a symbolic link refuses to stamp. The tree's own workspace file stays in force.
func wideEnvironment(goos string, tags string) []string {
	var environment []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		switch key {
		case "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS":
			continue
		}
		environment = append(environment, pair)
	}
	goFlags := "-buildvcs=false"
	if tags != "" {
		goFlags = "-tags=" + tags + " " + goFlags
	}
	return append(environment, "GOOS="+goos, "GOARCH=arm64", "CGO_ENABLED=0", "GOFLAGS="+goFlags)
}

// loadTree is tree's `./...` with its tests, loaded whole under environment: every package it reaches, with syntax
// and types. A package with errors is kept, and skipped when compared.
func loadTree(test *testing.T, tree string, environment []string) loadedBuild {
	test.Helper()
	roots, loadError := packages.Load(&packages.Config{
		Mode:  packages.LoadAllSyntax | packages.NeedTypesInfo,
		Tests: true,
		Dir:   tree,
		Env:   environment,
	}, "./...")
	if loadError != nil {
		test.Fatal(loadError)
	}
	loaded := loadedBuild{}
	packages.Visit(roots, nil, func(visited *packages.Package) { loaded[visited.ID] = visited })
	return loaded
}

// The wide run: the tree COHERE_GO_BUILD_DEPENDENCE_IR names, loaded under the host and under each build
// COHERE_GO_BUILD_DEPENDENCE_IR_BUILDS names, one build at a time beside the host, every independent unit's functions
// compared. It reports the counts, every difference in an independent function (the first 200 listed beside their
// count), and how many dependent functions differ, and fails on any difference.
func TestEveryIndependentFunctionLowersTheSameInATree(test *testing.T) {
	test.Parallel()

	tree := os.Getenv(wideTreeVariable)
	if tree == "" {
		test.Skipf("%s names no tree", wideTreeVariable)
	}
	started := time.Now()
	host := loadTree(test, tree, wideEnvironment("darwin", ""))
	hostFlows := controlFlowsOf(test, host)
	test.Logf("host darwin: %d packages loaded, ctrlflow on %d, in %s", len(host), len(hostFlows),
		time.Since(started).Round(time.Second))
	for name := range strings.SplitSeq(os.Getenv(wideBuildsVariable), ",") {
		wide, isKnown := wideBuilds[name]
		if !isKnown {
			test.Fatalf("no build named %q; the builds are %q", name, slices.Sorted(maps.Keys(wideBuilds)))
		}
		started := time.Now()
		built := loadTree(test, tree, wideEnvironment(wide.goos, wide.tags))
		buildFlows := controlFlowsOf(test, built)
		dependence := graphOf(host, nil).Against(differenceOf(host, built))
		dependentUnits := 0
		for _, unit := range dependence.Units() {
			if unit.Dependent {
				dependentUnits++
			}
		}
		tally := compareBuilds(host, built, hostFlows, buildFlows, dependence)
		test.Logf("build %s: %d packages loaded, ctrlflow on %d; %d of %d units dependent; %d packages compared, "+
			"%d skipped; %d independent functions and initializer segments compared (%d of them segments), %d "+
			"instructions, %d synthetic functions reached, %d functions or segments without a counterpart; %d "+
			"differences in independent functions; %d of %d package-level initializer segments differ; %d of %d "+
			"dependent functions and segments lower differently; %s, peak heap %s",
			name, len(built), len(buildFlows), dependentUnits, len(dependence.Units()), tally.packages,
			len(tally.skipped), tally.independent, tally.segments, tally.instructions, tally.synthetic, tally.unpaired,
			len(tally.findings), len(tally.packageLevelDiffer), tally.packageLevel, tally.dependentDiffer,
			tally.dependent, time.Since(started).Round(time.Second), heapInUse())
		test.Logf("build %s skipped: %q", name, tally.skipped)
		for index, finding := range tally.findings {
			if index == wideDifferencesListed {
				test.Logf("build %s: %d more differences not listed", name, len(tally.findings)-index)
				break
			}
			test.Logf("build %s difference %d: %s", name, index+1, describeFinding(finding))
		}
		// A segment of no unit, the frame or the imports' initializers, differs wherever a file only one build
		// compiles imports a package the others don't; it is listed apart, as no unit's verdict answers for it.
		for _, finding := range tally.packageLevelDiffer {
			test.Logf("build %s package-level segment: %s", name, describeFinding(finding))
		}
		for _, unknown := range slices.Sorted(maps.Keys(tally.unknownFields)) {
			test.Errorf("build %s: an instruction field of a kind the comparison does not render: %s", name, unknown)
		}
		if len(tally.findings) > 0 {
			test.Errorf("build %s: %d differences in independent functions", name, len(tally.findings))
		}
		if dependentUnits > 0 && tally.dependentDiffer == 0 {
			test.Errorf("build %s: block 1 found %d dependent units and no dependent function lowers differently, "+
				"so the comparison sees nothing", name, dependentUnits)
		}
	}
}

// heapInUse is the heap the process holds now, for the wide run's log.
func heapInUse() string {
	var statistics runtime.MemStats
	runtime.ReadMemStats(&statistics)
	return fmt.Sprintf("%.1f GB", float64(statistics.HeapInuse)/(1<<30))
}
