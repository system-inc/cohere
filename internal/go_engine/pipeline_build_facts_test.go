package go_engine

import (
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules"
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

// windowsCensus is the facts census of the windows build of the fixture under testdata, under `--other-builds
// --build-dependence`, configure given the pipeline as windowsFacts gives it, and the pipeline it ran.
func windowsCensus(test *testing.T, fixture string, configure func(*pipeline)) (*pipeline, contract.DependenceFacts) {
	test.Helper()
	engine, windows := windowsFacts(test, fixture, configure, exitClean, "--other-builds", "--build-dependence")
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

// A plain run under `--other-builds` compares no facts: it builds no graph, collects no facts from the host's lint or
// any build's, and its builds restore from the host's run as they always have.
func TestAPlainRunComparesNoFacts(test *testing.T) {
	test.Parallel()

	engine, windows := windowsFacts(test, "build-facts", nil, exitClean, "--other-builds")
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
// its graph then holding os, or restores it from the binary's table, when the call is found only through the standard
// library's graph (#ss4t5g6). The workspace's counts are what the census wrote before every build shared one call
// index (#f9br7sg); the dependencies' are the standard library's of the host's platform, so they are left to the log.
func TestTheFactsCensusFollowsACallerChainAcrossBothGraphs(test *testing.T) {
	test.Parallel()

	table, _ := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	for _, each := range []struct {
		name      string
		configure func(*pipeline)
		restores  bool
	}{
		{name: "checked from source"},
		{name: "restored from the table", configure: readingTableAs(table, engineTestsStamp, ""), restores: true},
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

// A fact the host holds by a package-level object's path alone is keyed by that path as the object's name, which is
// exact, and is compared with the build's fact about the object of that name in its unit (#01chv64).
func TestAPathOnlyPackageLevelFactIsCompared(test *testing.T) {
	test.Parallel()

	key, isKeyed := factKeyByName(pathOnlyFact("fatal"), &objectpath.Encoder{})
	if !isKeyed || key.local != "fatal" || key.name != "fatal" {
		test.Fatalf("want the path keyed as the object's name, got %+v (keyed %t)", key, isKeyed)
	}
	// Moved to the unit declaring it, as byDeclaringUnit moves both sides.
	key.file, key.offset, key.local = "print.go", 120, ""
	hosted := map[factKey]check.ExportedFact{key: pathOnlyFact("fatal")}
	built := map[factKey]check.ExportedFact{key: {Analyzer: "ctrlflow", FactType: "noReturn"}}
	keptHosted, keptBuilt, left := withholdNameless(hosted, built)
	if _, isHosted := keptHosted[key]; !isHosted || len(keptBuilt) != 1 || left != 0 {
		test.Errorf("want the fact compared, both sides kept, got %d host and %d build facts, %d left out",
			len(keptHosted), len(keptBuilt), left)
	}
}

// A fact the host holds by a method's path alone has no name, since the path indexes into a method set another build's
// files can change, so it is never paired, even in a unit whose one unclaimed build fact of its kind is the likeliest
// match: both are left out and counted (#01chv64).
func TestAPathOnlyMethodFactIsUnkeyed(test *testing.T) {
	test.Parallel()

	key, isKeyed := factKeyByName(pathOnlyFact("pp.M3"), &objectpath.Encoder{})
	if !isKeyed || key.local != "pp.M3" || key.name != "" {
		test.Fatalf("want the path keyed with no name, got %+v (keyed %t)", key, isKeyed)
	}
	key.file, key.offset, key.local = "print.go", 900, ""
	builtKey := key
	builtKey.name = "badVerb"
	hosted := map[factKey]check.ExportedFact{key: pathOnlyFact("pp.M3")}
	built := map[factKey]check.ExportedFact{builtKey: {Analyzer: "ctrlflow", FactType: "noReturn"}}
	keptHosted, keptBuilt, left := withholdNameless(hosted, built)
	if len(keptHosted) != 0 || len(keptBuilt) != 0 || left != 2 {
		test.Errorf("want both facts left out and counted, got %d host and %d build facts, %d left out",
			len(keptHosted), len(keptBuilt), left)
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
	graph.AddPackage(host, false)
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
	declaring := &byDeclaringUnit{standardLibrary: standardLibrary}
	encoded, isHeld, isPlaced := declaring.answerOf(answers, check.ExportedFact{Package: build,
		Analyzer: "function_purity", FactType: "answer", Object: method, Answer: true})
	if !isPlaced || !isHeld || string(encoded) != "its own" {
		test.Errorf("Process.%s, %s on darwin and %s on windows: want the host's answer about it, got %q (held %t, "+
			"placed %t)", method.Name(), hostPath, buildPath, encoded, isHeld, isPlaced)
	}
}
