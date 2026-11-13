package go_engine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// windowsFacts runs the engine over a copy of the build-facts fixture with extra after the root and returns the
// pipeline it ran and the windows build's record, its second.
func windowsFacts(test *testing.T, wantExit int, extra ...string) (*pipeline, contract.BuildRecord) {
	test.Helper()
	engine, stream := runOver(test, copyFixture(test, filepath.Join("testdata", "build-facts")), wantExit, extra...)
	lint := lintRecordOf(test, stream)
	if len(lint.Builds) != 2 {
		test.Fatalf("want the host's build and windows's, got the records %+v", lint.Builds)
	}
	return engine, lint.Builds[1]
}

// windowsCensus is the facts census of the windows build of the build-facts fixture, under `--other-builds
// --build-dependence`.
func windowsCensus(test *testing.T) contract.DependenceFacts {
	test.Helper()
	_, windows := windowsFacts(test, exitClean, "--other-builds", "--build-dependence")
	if windows.Dependence == nil || windows.Dependence.Facts == nil {
		test.Fatalf("the windows build's record has no facts census: %+v", windows.Dependence)
	}
	test.Logf("the windows build's facts census: %+v", *windows.Dependence.Facts)
	return *windows.Dependence.Facts
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

	census := windowsCensus(test)
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

	census := windowsCensus(test)
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

	engine, windows := windowsFacts(test, exitClean, "--other-builds")
	if engine.dependence != nil || windows.Dependence != nil || engine.keepsHostRestoreOff {
		test.Errorf("a plain run built the graph (%t), wrote the dependence %+v, or kept the host's restore off (%t)",
			engine.dependence != nil, windows.Dependence, engine.keepsHostRestoreOff)
	}
	for _, run := range engine.buildRuns {
		if run.judged != nil || run.result != nil && run.result.Facts != nil {
			test.Errorf("a plain run's build %s judged its dependence or collected %d facts", run.build,
				len(run.result.Facts))
		}
	}
}
