package odd_slice_argument

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa5012"

	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/needs"
)

// factsDifferentialVariable names the tree the fact differential runs over: root for cohere's own module, or a
// directory, whose every package it loads as the engine does. The standard library is its source folder, under
// `GOROOT` set to the toolchain it belongs to.
const factsDifferentialVariable = "COHERE_GO_IR_FACTS_DIFFERENTIAL"

// differentialTree is the tree factsDifferentialVariable names, as a directory, or a skip when it is unset, since a
// tree takes minutes.
func differentialTree(test *testing.T) string {
	test.Helper()
	directory := needs.OptIn(
		test,
		factsDifferentialVariable,
		"a differential of the port's facts against the original's over root or a directory",
	)
	if directory != "root" {
		return directory
	}
	// This package sits four directories below the module root.
	root, rootError := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if rootError != nil {
		test.Fatal(rootError)
	}
	return root
}

// The fact differential (#w7zmvat): over the whole tree COHERE_GO_IR_FACTS_DIFFERENTIAL names, and every package it
// imports, staticcheck's SA5012 on buildir and the port on the engine's own intermediate representation export the
// same marks on every package, failing on the first that differs. The original is staticcheck's own analyzer rather
// than a kept copy of the port as it ran on buildir: its mark prints as the port's does, which is all the comparison
// reads, and the port on buildir differed from it only in reporting a go or defer statement's call, never in a mark.
func TestPortExportsTheOriginalsFactsOverATree(test *testing.T) {
	test.Parallel()
	parity.RequireIdenticalCorpusFacts(test, sa5012.Analyzer, pushAnalyzer, differentialTree(test))
}

// Over the same tree, the original and the port find the same findings, every field of each, and fail to run on the
// same packages, since the findings are what the marks are for. RequireIdenticalCorpusFacts compares marks alone, so
// this runs the pair again through parity.Analyze, the census's comparison. A go or defer statement passing an odd
// slice to a marked parameter would panic the original here, failing the test, the one place the two are known to
// part (TestPortReportsWhereTheOriginalPanics).
func TestPortFindsTheOriginalsFindingsOverATree(test *testing.T) {
	test.Parallel()
	directory := differentialTree(test)
	workspace, workspaceError := project.ReadWorkspace(directory)
	if workspaceError != nil {
		test.Fatalf("reading the modules of %s: %v", directory, workspaceError)
	}
	loaded, loadError := load.Packages(workspace.Root, []string{"./..."}, workspace.Environment(), nil)
	if loadError != nil {
		test.Fatalf("loading %s: %v", workspace.Root, loadError)
	}
	if len(loaded.Packages) == 0 {
		test.Fatalf("loading %s found no package, so the comparison would prove nothing", workspace.Root)
	}
	outcomes, analyzeError := parity.Analyze([]parity.Pair{{Name: "SA5012", Original: sa5012.Analyzer, Port: Analyzer}},
		loaded.Packages)
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", workspace.Root, analyzeError)
	}
	outcome := outcomes[0]
	test.Logf("%s: %d packages analyzed; the original found %d and did not run on %d, the port found %d and did not "+
		"run on %d", workspace.Root, len(loaded.Packages), len(outcome.Original.Findings),
		len(outcome.Original.Failures), len(outcome.Port.Findings), len(outcome.Port.Failures))
	for _, finding := range parity.Difference(outcome.Original.Findings, outcome.Port.Findings) {
		test.Errorf("the original alone found %s", finding)
	}
	for _, finding := range parity.Difference(outcome.Port.Findings, outcome.Original.Findings) {
		test.Errorf("the port alone found %s", finding)
	}
	if originalFailed, portFailed := failedPackages(outcome.Original), failedPackages(outcome.Port); !slices.Equal(
		originalFailed, portFailed) {
		test.Errorf("the original did not run on %v, and the port on %v", originalFailed, portFailed)
	}
}

// failedPackages is the packages a side did not run on, sorted. The two sides' errors there need not read alike, since
// each names the analyzer it requires that failed.
func failedPackages(side parity.Side) []string {
	packages := map[string]bool{}
	for _, failure := range side.Failures {
		packages[failure.Package] = true
	}
	return slices.Sorted(maps.Keys(packages))
}
