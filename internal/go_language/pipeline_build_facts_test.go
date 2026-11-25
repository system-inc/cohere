package go_language

import (
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_language/build_dependence"
	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules"
)

// windowsFacts runs the engine over a copy of the fixture under testdata with the findings cache off, configure, when
// not nil, given the pipeline before it runs, and extra after the root, and returns the pipeline it ran and the windows
// build's record, its second.
func windowsFacts(
	test *testing.T,
	fixture string,
	configure func(*pipeline),
	wantExit int,
	extra ...string,
) (*pipeline, contract.BuildRecord) {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", fixture))
	engine, stream := runOverConfigured(test, root, wantExit, configure, append([]string{"--no-cache"}, extra...)...)
	lint := lintRecordOf(test, normalize(stream, root))
	if len(lint.Builds) != 2 {
		test.Fatalf("want the host's build and windows's, got the records %+v", lint.Builds)
	}
	return engine, lint.Builds[1]
}

// windowsCensus is the facts census of the windows build of the fixture under testdata, under `--build-dependence`,
// configure given the pipeline as windowsFacts gives it, and the pipeline it ran.
func windowsCensus(test *testing.T, fixture string, configure func(*pipeline)) (*pipeline, contract.DependenceFacts) {
	test.Helper()
	engine, windows := windowsFacts(test, fixture, configure, exitClean, "--build-dependence")
	if windows.Dependence == nil || windows.Dependence.Facts == nil {
		test.Fatalf("the windows build's record has no facts census: %+v", windows.Dependence)
	}
	test.Logf("the windows build's facts census: %+v", *windows.Dependence.Facts)
	return engine, *windows.Dependence.Facts
}

// noReturnKind is the census's count of ctrlflow's no-return facts.
func noReturnKind(test *testing.T, census contract.DependenceFacts) contract.FactKindCounts {
	test.Helper()
	for _, kind := range census.Kinds {
		if kind.Analyzer == "ctrlflow" && strings.HasSuffix(kind.FactType, ".noReturn") {
			return kind
		}
	}
	test.Fatalf("the census counts no ctrlflow no-return fact: %+v", census.Kinds)
	return contract.FactKindCounts{}
}

// On windows, platformFinish calls os.Exit, so Finish, which calls it from the shared file, never returns there, and
// neither does Run, which calls Finish. Run is independent by its types, so it is the caller a model judging each unit
// once would judge again: the census counts the no-return facts of Finish and Run as different, the host having none,
// Finish and Run as declaring an object whose fact differs, and Run as the one caller to judge again, first-order and
// at the bound, of the three independent units that call a dependent one: Run, Halt and Doubled.
func TestADifferingNoReturnFactIsCountedAndItsCallerJudgedAgain(test *testing.T) {
	test.Parallel()

	_, census := windowsCensus(test, "build-facts", nil)
	noReturn := noReturnKind(test, census)
	examples := strings.Join(noReturn.Examples, "\n")
	if noReturn.Different != 2 || !strings.Contains(examples, " Finish at ") ||
		!strings.Contains(examples, " Run at ") {
		test.Errorf("want the no-return facts of Finish and Run counted as different, got %+v", noReturn)
	}
	workspace := census.Workspace
	if workspace.DifferingUnits != 2 || workspace.FirstOrderUnits != 1 || workspace.BoundUnits != 1 ||
		workspace.CallerUnits != 3 {
		test.Errorf("want Finish and Run differing, and Run alone to judge again, first-order and at the bound, of 3 "+
			"independent callers of a dependent unit, got %+v", workspace)
	}
}

// Stop reads code, a constant windows declares apart, so its body is dependent, but it calls os.Exit in every build,
// so its no-return fact, and Halt's, its caller's, are the same in both: the census counts both as equal, names
// neither as differing, and Halt, an independent caller of the dependent Stop, is no caller to judge again (the one
// caller to judge again is Run, as the test above pins). Width, whose body reads size, exports no fact in either
// build, so no fact of it or of Doubled is counted at all.
func TestAnEqualFactLeavesItsCallerAlone(test *testing.T) {
	test.Parallel()

	_, census := windowsCensus(test, "build-facts", nil)
	noReturn := noReturnKind(test, census)
	if noReturn.WorkspaceCompared != 4 || noReturn.WorkspaceEqual != 2 || noReturn.WorkspaceDifferent != 2 {
		test.Errorf("want the workspace's no-return facts of Stop and Halt compared and equal beside the 2 that "+
			"differ, got %+v", noReturn)
	}
	for _, kind := range census.Kinds {
		for _, example := range kind.Examples {
			for _, name := range []string{" Stop at ", " Halt at ", " Width at ", " Doubled at "} {
				if strings.Contains(example, name) {
					test.Errorf("%s %s counts a fact of%sas different: %s", kind.Analyzer, kind.FactType,
						strings.TrimSuffix(name, "at "), example)
				}
			}
		}
	}
	if census.Workspace.FirstOrderUnits != 1 || census.Workspace.BoundUnits != 1 {
		test.Errorf("want Run alone to judge again, Halt not, got %+v", census.Workspace)
	}
}

// A plain run of every build compares no facts: it builds no graph, collects no facts from the host's lint or
// any build's, and its builds restore from the host's run as they always have.
func TestAPlainRunComparesNoFacts(test *testing.T) {
	test.Parallel()

	engine, windows := windowsFacts(test, "build-facts", nil, exitClean)
	if engine.dependence != nil || windows.Dependence != nil || engine.censusKeepsHostRestoreOff {
		test.Errorf("a plain run built the graph (%t), wrote the dependence %+v, or kept the host's restore off (%t)",
			engine.dependence != nil, windows.Dependence, engine.censusKeepsHostRestoreOff)
	}
	for _, run := range engine.buildRuns {
		if run.judged != nil || run.result != nil && run.result.Facts != nil {
			test.Errorf("a plain run's build %s judged its dependence or collected %d facts", run.build,
				len(run.result.Facts))
		}
	}
}

// On windows, Finish never returns, Maybe calls it only when asked to and so returns in every build, and Twice calls
// Maybe: the census counts Finish as differing, Maybe as its one first-order caller, and Twice beside Maybe at the
// bound, two calls from Finish, so the bound passes the first order. Process calls os.Getpid, whose shared file reads
// syscall's, which each platform's file declares, so beside Maybe, a caller of the dependent Finish, it is an
// independent caller of a dependent unit. It counts alike whether the host checks the standard library from source,
// its graph then holding os, or restores it from the system cache's table, when the call is found only through the
// standard library's graph (#ss4t5g6). The workspace's counts are what the census wrote before every build shared one
// call index (#f9br7sg); the dependencies' are the standard library's of the host's platform, so they are left to the
// log.
func TestTheFactsCensusFollowsACallerChainAcrossBothGraphs(test *testing.T) {
	test.Parallel()

	// The case checked from source computes the standard library, beside the case restoring it from the table.
	coldSystemCacheFor(test)
	filled, _ := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	for _, each := range []struct {
		name      string
		configure func(*pipeline)
		restores  bool
	}{
		{name: "checked from source"},
		{name: "restored from the table", configure: readingSystemCache(filled, ""), restores: true},
	} {
		engine, census := windowsCensus(test, "build-facts-chain", each.configure)
		hasGraph := engine.dependence.standardLibrary != nil && engine.dependence.standardLibrary.graph != nil
		if hasGraph != each.restores {
			test.Fatalf("%s: want a graph of the standard library %t, got %t", each.name, each.restores, hasGraph)
		}
		wantWorkspace := contract.FactCallers{DifferingUnits: 1, DifferingNodes: 10, FirstOrderUnits: 1,
			FirstOrderNodes: 16, BoundUnits: 2, BoundNodes: 31, CallerUnits: 2, CallerNodes: 31}
		if census.Workspace != wantWorkspace {
			test.Errorf("%s: want Finish differing, Maybe first-order, Maybe and Twice at the bound, and Maybe and "+
				"Process calling a dependent unit, %+v, got %+v", each.name, wantWorkspace, census.Workspace)
		}
		noReturn := noReturnKind(test, census)
		wantExamples := []string{"example.com/chain, Finish at /project/chain.go:447: host none, build noReturn"}
		if noReturn.Different != 1 || noReturn.WorkspaceCompared != 1 || noReturn.WorkspaceEqual != 0 ||
			noReturn.WorkspaceDifferent != 1 || !slices.Equal(noReturn.Examples, wantExamples) {
			test.Errorf("%s: want Finish's no-return fact the workspace's one compared, and different, got %+v",
				each.name, noReturn)
		}
	}
}

// pathOnlyFact is a fact the host restored of package fmt about the object at path, which its export data does not
// hold, so it carries the path alone, as a table's entry holds it.
func pathOnlyFact(path string) check.ExportedFact {
	return check.ExportedFact{Package: &packages.Package{ID: "fmt", PkgPath: "fmt"}, Analyzer: "ctrlflow",
		FactType: "noReturn", ObjectPath: objectpath.Path(path)}
}

// toUnit moves a key to the unit at offset 900 of print.go, as byDeclaringUnit.unitKey moves a placed key.
func toUnit(key factKey) (factKey, bool) {
	key.file, key.offset, key.local, key.byUnit = "print.go", 900, "", true
	return key, true
}

// builtAt is the build's fact key, by position, of a noReturn fact of fmt about the object name at offset.
func builtAt(name string, offset int) factKey {
	return factKey{packageIdentifier: "fmt", file: "print.go", offset: offset, name: name, analyzer: "ctrlflow",
		factType: "noReturn"}
}

// builtNoReturn is the build's noReturn fact of fmt, its value beside the point.
var builtNoReturn = check.ExportedFact{Analyzer: "ctrlflow", FactType: "noReturn"}

// A fact the host holds by a package-level object's path alone is keyed by that path as the object's name, which is
// exact, and is paired with the build's fact about the object of that name in its unit (#h660y7d).
func TestAPathOnlyPackageLevelFactIsCompared(test *testing.T) {
	test.Parallel()

	key, isKeyed := factKeyByName(pathOnlyFact("fatal"), &objectpath.Encoder{})
	if !isKeyed || key.local != "fatal" || key.name != "fatal" {
		test.Fatalf("want the path keyed as the object's name, got %+v (keyed %t)", key, isKeyed)
	}
	hosted, built, parts := pairByUnit(map[factKey]check.ExportedFact{key: pathOnlyFact("fatal")},
		map[factKey]check.ExportedFact{builtAt("fatal", 905): builtNoReturn}, toUnit, toUnit)
	paired, _ := toUnit(builtAt("fatal", 905))
	if _, isHosted := hosted[paired]; !isHosted || len(built) != 1 || unkeyedTotal(parts) != 0 {
		test.Errorf("want the fact paired, got %d host and %d build facts, %+v left out", len(hosted), len(built),
			parts)
	}
}

// A fact the host holds by a method's path alone has no name, since the path indexes into a method set another build's
// files can change, so it is never paired, even in a unit whose one unclaimed build fact of its kind is the likeliest
// match: both are left out, counted as path-only (#h660y7d).
func TestAPathOnlyMethodFactIsUnkeyed(test *testing.T) {
	test.Parallel()

	key, isKeyed := factKeyByName(pathOnlyFact("pp.M3"), &objectpath.Encoder{})
	if !isKeyed || key.local != "pp.M3" || key.name != "" {
		test.Fatalf("want the path keyed with no name, got %+v (keyed %t)", key, isKeyed)
	}
	hosted, built, parts := pairByUnit(map[factKey]check.ExportedFact{key: pathOnlyFact("pp.M3")},
		map[factKey]check.ExportedFact{builtAt("badVerb", 905): builtNoReturn}, toUnit, toUnit)
	if len(hosted) != 0 || len(built) != 0 || parts != (contract.UnkeyedFacts{PathOnly: 2}) {
		test.Errorf("want both facts left out as path-only, got %d host and %d build facts, %+v", len(hosted),
			len(built), parts)
	}
}

// Two facts the host holds by members' paths alone, of one kind, in one unit, meet under one key, and the unit loses
// them as a collision: the build's facts of that kind there are left out with them, never counted as differing with no
// host side (#h660y7d, the review's probe).
func TestTwoPathOnlyHostFactsInOneUnitWithholdTheBuilds(test *testing.T) {
	test.Parallel()

	first, _ := factKeyByName(pathOnlyFact("pp.F0"), &objectpath.Encoder{})
	second, _ := factKeyByName(pathOnlyFact("pp.F1"), &objectpath.Encoder{})
	hosted, built, parts := pairByUnit(map[factKey]check.ExportedFact{
		first: pathOnlyFact("pp.F0"), second: pathOnlyFact("pp.F1"),
	}, map[factKey]check.ExportedFact{builtAt("buf", 910): builtNoReturn, builtAt("arg", 920): builtNoReturn},
		toUnit, toUnit)
	if len(hosted) != 0 || len(built) != 0 || parts != (contract.UnkeyedFacts{Collisions: 4}) {
		test.Errorf("want all four facts left out as a collision, got %d host and %d build facts, %+v", len(hosted),
			len(built), parts)
	}
}

// Two facts of one kind that meet under one unit and name, on the host's side as on the build's, are told apart by
// nothing, so neither is paired: on the host's side the unit's build facts of the kind go with them, and on the
// build's the host's fact under that key does (#h660y7d).
func TestFactsMeetingUnderOneKeyAreNeverPaired(test *testing.T) {
	test.Parallel()

	named := func(path string) factKey {
		key := factKey{packageIdentifier: "fmt", offset: -1, local: path, name: "x", analyzer: "ctrlflow",
			factType: "noReturn"}
		return key
	}
	hosted, built, parts := pairByUnit(map[factKey]check.ExportedFact{
		named("pp.F0.F0"): pathOnlyFact("pp.F0.F0"), named("pp.F1.F0"): pathOnlyFact("pp.F1.F0"),
	}, map[factKey]check.ExportedFact{builtAt("x", 910): builtNoReturn, builtAt("y", 920): builtNoReturn},
		toUnit, toUnit)
	if len(hosted) != 0 || len(built) != 0 || parts != (contract.UnkeyedFacts{Collisions: 4}) {
		test.Errorf("host side meeting: want all four facts left out, got %d host and %d build facts, %+v",
			len(hosted), len(built), parts)
	}
	hosted, built, parts = pairByUnit(map[factKey]check.ExportedFact{named("pp.F0.F0"): pathOnlyFact("pp.F0.F0")},
		map[factKey]check.ExportedFact{builtAt("x", 910): builtNoReturn, builtAt("x", 930): builtNoReturn},
		toUnit, toUnit)
	if len(hosted) != 0 || len(built) != 0 || parts != (contract.UnkeyedFacts{Collisions: 3}) {
		test.Errorf("build side meeting: want all three facts left out, got %d host and %d build facts, %+v",
			len(hosted), len(built), parts)
	}
}

// A host fact no unit declares, its name not found in the graph, fails as the build's fact about the same object
// does: the build's fact of the same package, kind and name is left out with it, counted unplaced, rather than
// compared with nothing (#h660y7d).
func TestAnUnplacedHostFactWithholdsTheBuildsFactOfItsName(test *testing.T) {
	test.Parallel()

	unplacedHost := func(key factKey) (factKey, bool) { return factKey{}, false }
	key := factKey{packageIdentifier: "fmt", offset: -1, local: "pp.M9", name: "badVerb", analyzer: "ctrlflow",
		factType: "noReturn"}
	hosted, built, parts := pairByUnit(map[factKey]check.ExportedFact{key: pathOnlyFact("pp.M9")},
		map[factKey]check.ExportedFact{builtAt("badVerb", 905): builtNoReturn, builtAt("other", 920): builtNoReturn},
		unplacedHost, toUnit)
	other, _ := toUnit(builtAt("other", 920))
	if _, isKept := built[other]; len(hosted) != 0 || len(built) != 1 || !isKept ||
		parts != (contract.UnkeyedFacts{Unplaced: 2}) {
		test.Errorf("want the host's fact and the build's of its name left out, the other kept, got %d host and %d "+
			"build facts, %+v", len(hosted), len(built), parts)
	}
}

// loadedOs is the standard library's os package loaded from source for goos on arm64, cgo off, from a module of its
// own.
func loadedOs(test *testing.T, goos string) *packages.Package {
	test.Helper()
	module := test.TempDir()
	if writeError := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module cohere.invalid/answers\n\ngo 1.24\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	loaded, loadError := load.PackagesCached(module, []string{"os"},
		[]string{"GOOS=" + goos, "GOARCH=arm64", "CGO_ENABLED=0", "GOWORK=off"}, nil, load.Caching{WithoutTests: true})
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading os for %s: %v, %d errors", goos, loadError, len(loaded.Diagnostics))
	}
	var found *packages.Package
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
		if each.ID == "os" {
			found = each
		}
	})
	if found == nil {
		test.Fatalf("the load for %s holds no os", goos)
	}
	return found
}

// A restored package's answer about a method is the host's answer about that method, though the build's files change
// its type's method set (#01chv64): os.Process has other methods on windows than on darwin, so the windows build's
// objectpath of a method declared in a file both compile names another method in the darwin host's answers, and the
// census finds the host's answer by the unit declaring the method in the darwin graph instead, where the build's
// method sits.
func TestARestoredMethodAnswerMeetsItsOwnMethodWhereMethodSetsDiverge(test *testing.T) {
	test.Parallel()

	host, build := loadedOs(test, "darwin"), loadedOs(test, "windows")
	graph := build_dependence.NewGraph()
	graph.AddPackage(host, false, nil)
	standardLibrary := graph.Against(build_dependence.Difference{
		Packages:          map[string]build_dependence.PackageDifference{},
		HostOnlyPackages:  map[string]bool{},
		BuildOnlyPackages: map[string]bool{},
	})
	processOf := func(loaded *packages.Package) *types.Named {
		named, isNamed := loaded.Types.Scope().Lookup("Process").Type().(*types.Named)
		if !isNamed {
			test.Fatal("os.Process is no named type")
		}
		return named
	}
	hostProcess, buildProcess := processOf(host), processOf(build)
	var hostEncoder objectpath.Encoder
	var buildEncoder objectpath.Encoder
	var method *types.Func
	var hostPath objectpath.Path
	var buildPath objectpath.Path
	for built := range buildProcess.Methods() {
		file := build.Fset.Position(built.Pos()).Filename
		hosted, _, _ := types.LookupFieldOrMethod(hostProcess, true, host.Types, built.Name())
		hostedMethod, isMethod := hosted.(*types.Func)
		if !isMethod || !slices.Contains(host.GoFiles, file) {
			continue
		}
		hostMethodPath, hostError := hostEncoder.For(hostedMethod)
		buildMethodPath, buildError := buildEncoder.For(built)
		other, otherError := objectpath.Object(host.Types, buildMethodPath)
		if hostError == nil && buildError == nil && otherError == nil && hostMethodPath != buildMethodPath &&
			other.Name() != built.Name() {
			method, hostPath, buildPath = built, hostMethodPath, buildMethodPath
			break
		}
	}
	if method == nil {
		test.Fatal("no method of os.Process in a shared file has another objectpath on windows, so the case proves " +
			"nothing")
	}
	test.Logf("Process.%s is %s on darwin and %s on windows", method.Name(), hostPath, buildPath)
	answers := &fact_resolver.Answers{Values: map[string]map[objectpath.Path][]byte{
		"function_purity": {hostPath: []byte("its own"), buildPath: []byte("another method's")},
	}}
	// Through restoredAnswer, as compareFacts asks, the host having restored os and the standard library's graph
	// holding it.
	engine := &pipeline{dependence: &hostDependence{restoredAnswers: map[string]*fact_resolver.Answers{"os": answers},
		standardLibrary: &standardLibraryDependence{graph: graph, files: map[string][]string{"os": host.GoFiles}}}}
	hosted, isHeld, isPlaced := engine.restoredAnswer(check.ExportedFact{Package: build,
		Analyzer: "function_purity", FactType: "answer", Object: method, Answer: true}, &objectpath.Encoder{},
		&byDeclaringUnit{standardLibrary: standardLibrary})
	if !isPlaced || !isHeld || string(hosted.Encoded) != "its own" {
		test.Errorf("Process.%s, %s on darwin and %s on windows: want the host's answer about it, got %q (held %t, "+
			"placed %t)", method.Name(), hostPath, buildPath, hosted.Encoded, isHeld, isPlaced)
	}
}

// loadedSplit is the census-split fixture's package loaded from source for goos (loadedFromSource).
func loadedSplit(test *testing.T, root string, goos string) *packages.Package {
	test.Helper()
	return loadedFromSource(test, root, goos, "example.com/split")
}

// loadedFromSource is the package identifier of the fixture at root loaded from source for goos on arm64, cgo off.
func loadedFromSource(test *testing.T, root string, goos string, identifier string) *packages.Package {
	test.Helper()
	loaded, loadError := load.PackagesCached(root, []string{"./..."},
		[]string{"GOOS=" + goos, "GOARCH=arm64", "CGO_ENABLED=0", "GOWORK=off"}, nil, load.Caching{WithoutTests: true})
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the fixture for %s: %v, %d errors", goos, loadError, len(loaded.Diagnostics))
	}
	var found *packages.Package
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
		if each.ID == identifier {
			found = each
		}
	})
	if found == nil {
		test.Fatalf("the load for %s holds no %s", goos, identifier)
	}
	return found
}

// splitGraph is the census-split fixture's standard-library graph as host, darwin, loads it, judged with no difference.
func splitGraph(host *packages.Package) (*build_dependence.Graph, *build_dependence.Dependence) {
	graph := build_dependence.NewGraph()
	graph.AddPackage(host, false, nil)
	return graph, graph.Against(build_dependence.Difference{
		Packages:          map[string]build_dependence.PackageDifference{},
		HostOnlyPackages:  map[string]bool{},
		BuildOnlyPackages: map[string]bool{},
	})
}

// answerAbout is the host's answer, through restoredAnswer as compareFacts asks, about object of build's package, the
// darwin host holding answers and the census-split fixture's darwin graph.
func answerAbout(
	host *packages.Package,
	build *packages.Package,
	object types.Object,
	answers *fact_resolver.Answers,
) (check.ExportedFact, bool, bool) {
	graph, standardLibrary := splitGraph(host)
	engine := &pipeline{dependence: &hostDependence{
		restoredAnswers: map[string]*fact_resolver.Answers{host.ID: answers},
		standardLibrary: &standardLibraryDependence{graph: graph, files: map[string][]string{host.ID: host.GoFiles}},
	}}
	return engine.restoredAnswer(check.ExportedFact{Package: build, Analyzer: "odd_slice_argument", FactType: "answer",
		Object: object, Answer: true}, &objectpath.Encoder{}, &byDeclaringUnit{standardLibrary: standardLibrary})
}

// splitFunction is the function name that loaded, the census-split fixture's package, declares.
func splitFunction(test *testing.T, loaded *packages.Package, name string) *types.Func {
	test.Helper()
	function, isFunction := loaded.Types.Scope().Lookup(name).(*types.Func)
	if !isFunction {
		test.Fatalf("the fixture declares no function %s", name)
	}
	return function
}

// parameterAnswer is the host's answer (answerAbout) about the first parameter of the function name in build's
// package.
func parameterAnswer(
	test *testing.T,
	host *packages.Package,
	build *packages.Package,
	name string,
	answers *fact_resolver.Answers,
) (check.ExportedFact, bool, bool) {
	test.Helper()
	return answerAbout(host, build, splitFunction(test, build, name).Signature().Params().At(0), answers)
}

// A restored package's answer about a function's parameter, as odd_slice_argument answers, is placed through the unit
// declaring its function and its place in the signature, and compared with the host's answer about that parameter
// (#h660y7d): Shared is in a file both builds compile.
func TestAParameterAnswerIsComparedThroughItsFunctionsUnit(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "census-split"))
	host, build := loadedSplit(test, root, "darwin"), loadedSplit(test, root, "windows")
	hosted, isHeld, isPlaced := parameterAnswer(test, host, build, "Shared",
		&fact_resolver.Answers{Values: map[string]map[objectpath.Path][]byte{
			"odd_slice_argument": {"Shared.PA0": []byte("the host's")},
		}})
	if !isPlaced || !isHeld || string(hosted.Encoded) != "the host's" {
		test.Errorf("Shared's parameter: want the host's answer about it, got %q (held %t, placed %t)",
			hosted.Encoded, isHeld, isPlaced)
	}
}

// Two functions of one name in files of different builds share a path, Split.PA0 for each's parameter, though they are
// other code, so a build's answer about windows' never meets the darwin host's about darwin's: no unit of the host's
// graph holds windows' file, and the answer is not placed (#h660y7d).
func TestSameNamedFunctionsOfOtherBuildsNeverPair(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "census-split"))
	host, build := loadedSplit(test, root, "darwin"), loadedSplit(test, root, "windows")
	hosted, isHeld, isPlaced := parameterAnswer(test, host, build, "Split",
		&fact_resolver.Answers{Values: map[string]map[objectpath.Path][]byte{
			"odd_slice_argument": {"Split.PA0": []byte("darwin's")},
		}})
	if isPlaced || isHeld {
		test.Errorf("windows' Split: want its parameter's answer not placed, got %q (held %t, placed %t)",
			hosted.Encoded, isHeld, isPlaced)
	}
}

// A function's declaration declares the members of anonymous types in its signature beside the function, so the unit
// of Serve declares Handle, its second parameter's method, at Serve.PA1.M0, and Handle's own parameter beside it. The
// host's answer about either is no answer about Serve or Serve's own parameter, though the two sit in one unit, at the
// function and at the first parameter's place: a build's answer about Serve, or about dst, is placed and finds none
// (#h660y7d, the second read's must).
func TestAnAnonymousInterfacesMethodNeverAnswersForItsFunction(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "census-split"))
	host, build := loadedSplit(test, root, "darwin"), loadedSplit(test, root, "windows")
	hostServe := splitFunction(test, host, "Serve")
	handler, isInterface := hostServe.Signature().Params().At(1).Type().(*types.Interface)
	if !isInterface || handler.NumMethods() != 1 {
		test.Fatal("Serve's second parameter is no interface of one method, so the case proves nothing")
	}
	var encoder objectpath.Encoder
	methodPath, methodError := encoder.For(handler.Method(0))
	parameterPath, parameterError := encoder.For(handler.Method(0).Signature().Params().At(0))
	if methodError != nil || parameterError != nil || methodPath != "Serve.PA1.M0" ||
		parameterPath != "Serve.PA1.M0.PA0" {
		test.Fatalf("want Handle at Serve.PA1.M0 and its parameter at Serve.PA1.M0.PA0, got %q (%v) and %q (%v)",
			methodPath, methodError, parameterPath, parameterError)
	}
	_, standardLibrary := splitGraph(host)
	serveUnit, isServeDeclared := standardLibrary.UnitDeclaringName(host.PkgPath, "Serve")
	handleUnit, isHandleDeclared := standardLibrary.UnitDeclaringName(host.PkgPath, string(methodPath))
	if !isServeDeclared || !isHandleDeclared || serveUnit != handleUnit {
		test.Fatalf("want Serve's unit to declare Handle beside Serve, got units %d (%t) and %d (%t), so the case "+
			"proves nothing", serveUnit, isServeDeclared, handleUnit, isHandleDeclared)
	}
	buildServe := splitFunction(test, build, "Serve")
	for _, each := range []struct {
		about  string
		object types.Object
		path   objectpath.Path
	}{
		{about: "Serve", object: buildServe, path: methodPath},
		{about: "Serve's dst", object: buildServe.Signature().Params().At(0), path: parameterPath},
	} {
		hosted, isHeld, isPlaced := answerAbout(host, build, each.object,
			&fact_resolver.Answers{Values: map[string]map[objectpath.Path][]byte{
				"odd_slice_argument": {each.path: []byte("Handle's")},
			}})
		if !isPlaced || isHeld {
			test.Errorf("%s, the host answering only at %s: want it placed and holding none, got %q (held %t, "+
				"placed %t)", each.about, each.path, hosted.Encoded, isHeld, isPlaced)
		}
	}
}

// A host's fact about a parameter is declared by no unit of its own, so it is moved to the unit declaring its function,
// where the build's fact about the same parameter sits by position and meets it by name (#h660y7d); a parameter of an
// anonymous interface's method in a signature, whose function's path is no declared function's, is not placed.
func TestAHostFactAboutAParameterIsPlacedByItsFunction(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "census-split"))
	host, build := loadedSplit(test, root, "darwin"), loadedSplit(test, root, "windows")
	_, standardLibrary := splitGraph(host)
	declaring := &byDeclaringUnit{standardLibrary: standardLibrary}
	keyOf := func(loaded *packages.Package, object types.Object) check.ExportedFact {
		return check.ExportedFact{Package: loaded, Analyzer: "odd_slice_argument", FactType: "answer", Object: object,
			Answer: true}
	}
	hostDestination := splitFunction(test, host, "Shared").Signature().Params().At(0)
	hostKey, isNamed := factKeyByName(keyOf(host, hostDestination), &objectpath.Encoder{})
	if !isNamed || hostKey.local != "Shared.PA0" || hostKey.name != "dst" {
		test.Fatalf("want the host's dst named Shared.PA0, got %+v (named %t)", hostKey, isNamed)
	}
	buildKey, isKeyed := factKeyOf(keyOf(build, splitFunction(test, build, "Shared").Signature().Params().At(0)))
	if !isKeyed {
		test.Fatal("the build's dst has no position")
	}
	hosted, isHosted := declaring.hosted(hostKey)
	built, isBuilt := declaring.built(buildKey)
	if !isHosted || !isBuilt || hosted != built {
		test.Errorf("Shared's dst: want the host's fact and the build's under one key, got %+v (placed %t) and %+v "+
			"(placed %t)", hosted, isHosted, built, isBuilt)
	}
	handler, isInterface := splitFunction(test, host, "Serve").Signature().Params().At(1).Type().(*types.Interface)
	if !isInterface || handler.NumMethods() != 1 {
		test.Fatal("Serve's second parameter is no interface of one method, so the case proves nothing")
	}
	bufferKey, _ := factKeyByName(keyOf(host, handler.Method(0).Signature().Params().At(0)), &objectpath.Encoder{})
	if placed, isPlaced := declaring.hosted(bufferKey); isPlaced {
		test.Errorf("Handle's buf, at %s: want it not placed, got %+v", bufferKey.local, placed)
	}
}

// A unit where the host's facts of one kind both meet under one name and are held by a path alone withholds the
// build's unclaimed facts of the kind as a collision, whichever group the census ranges over first, so every run
// counts them alike (#h660y7d).
func TestAWithheldUnitIsCountedAsItsCollisionInEveryOrder(test *testing.T) {
	test.Parallel()

	named := func(path string, name string) factKey {
		return factKey{packageIdentifier: "fmt", offset: -1, local: path, name: name, analyzer: "ctrlflow",
			factType: "noReturn"}
	}
	host := map[factKey]check.ExportedFact{
		named("pp.F0.F0", "x"): pathOnlyFact("pp.F0.F0"),
		named("pp.F1.F0", "x"): pathOnlyFact("pp.F1.F0"),
		named("pp.M3", ""):     pathOnlyFact("pp.M3"),
	}
	want := contract.UnkeyedFacts{PathOnly: 1, Collisions: 3}
	// Go ranges over a map in a new order each time, so a hundred rounds meet both orders all but surely.
	for round := range 100 {
		hosted, built, parts := pairByUnit(host,
			map[factKey]check.ExportedFact{builtAt("y", 920): builtNoReturn}, toUnit, toUnit)
		if len(hosted) != 0 || len(built) != 0 || parts != want {
			test.Fatalf("round %d: want every fact left out, counted %+v, got %d host and %d build facts, %+v", round,
				want, len(hosted), len(built), parts)
		}
	}
}
