package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
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
