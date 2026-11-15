package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A run under `--other-builds` over a module whose census needs two extra builds starts every go command it needs
// before the host's first load (#sktb2y8): the host's lister state, and each build's go env and lister state, so its
// lint record counts no go command started after that load began. On darwin each child started once the heap has grown
// makes every resident page fault again. A build that read its go env or its lister state itself, as each did before,
// would be counted and named there.
func TestARunWithBuildsStartsEveryGoCommandBeforeItsFirstLoad(test *testing.T) {
	test.Parallel()
	engine, stream := runOver(test, copyFixture(test, filepath.Join("testdata", "other-builds")), exitFindings,
		"--other-builds")
	if len(engine.buildRuns) != 3 {
		test.Fatalf("the run checked %d builds, want the host's and two extra", len(engine.buildRuns))
	}
	lint := lintRecordOf(test, stream)
	if lint.ChildProcessesAfterLoad != 0 || len(lint.ChildProcessesAfterLoadNamed) != 0 {
		test.Errorf("the run started %d go commands after its first load began, want 0: %s",
			lint.ChildProcessesAfterLoad, strings.Join(lint.ChildProcessesAfterLoadNamed, "; "))
	}
}

// A run of one build under `--timing` starts nothing before its first load, and its lister's two go commands start
// inside it, which the lint record counts and names, so the count is of commands that ran and not a field never set.
// `--timing` is the run that still shows the count live: it keeps the lister state inside the first load, so the load's
// measurement of its children's time holds its own commands, where a run not timed fetches it beside its census and
// its reads (#q9ezwvw) and reads 0, as the end-to-end streams pin.
func TestARunOfOneBuildNamesItsListersCommands(test *testing.T) {
	test.Parallel()
	_, stream := runOver(test, copyFixture(test, filepath.Join("testdata", "clean")), exitClean, "--timing")
	lint := lintRecordOf(test, stream)
	named := slices.Sorted(slices.Values(lint.ChildProcessesAfterLoadNamed))
	want := []string{"go list -e -f", "go list -m -json"}
	if lint.ChildProcessesAfterLoad != len(want) || !slices.Equal(named, want) {
		test.Errorf("the run counted %d go commands after its first load began, named %q, want %d, named %q",
			lint.ChildProcessesAfterLoad, named, len(want), want)
	}
}
