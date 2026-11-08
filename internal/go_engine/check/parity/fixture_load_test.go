package parity

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// A folder of testdata holding a go.mod loads as a module, from its folder: module_one imports its package inner by
// the module's path, which resolves nowhere in the tree under src, and inner's call is found too. Its files are named
// by their paths below its folder after the module's pattern, and the tree's beside them by their paths below src.
func TestFindingsLoadsAFolderHoldingAGoModAsAModule(test *testing.T) {
	test.Parallel()
	_, portFindings := Findings(test, original, port, analysistest.TestData(), "same", "module_one")
	wantedPort := []Finding{
		{File: "module_one/inner/inner.go", Line: 8, Column: 2, Message: "call to shared"},
		{File: "module_one/module_one.go", Line: 14, Column: 2, Message: "call to shared"},
		{File: "module_one/module_one.go", Line: 15, Column: 2, Message: "call to gained"},
		{File: "same/same.go", Line: 7, Column: 2, Message: "call to shared"},
		{File: "same/same.go", Line: 8, Column: 2, Message: "call to shared"},
	}
	if !reflect.DeepEqual(portFindings, wantedPort) {
		test.Errorf("the port's findings are %v, want %v", portFindings, wantedPort)
	}
}

func TestRequireUpstreamWantsRunsAModuleFromItsFolder(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireUpstreamWants(recording, port, analysistest.TestData(), "wants", "module_one")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireUpstreamWants(recording, original, analysistest.TestData(), "module_one")
	}), "module_one.go:15: no diagnostic was reported matching `call to gained`")
}

// RequireIdentical compares a module's findings with the tree's, in one comparison.
func TestRequireIdenticalComparesAModuleWithTheTree(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdentical(recording, original, port, analysistest.TestData(), "same", "module_one")
	}), "the port found module_one/module_one.go:15:2: call to gained, and the original did not")
}

// Two fixture packages each hold a file of one name, and the original reports a call in one where the port reports
// the same line and message in the other. Each file is named by its path below testdata's src, so the two never pair.
func TestRequireIdenticalTellsApartFilesOfOneNameInTwoPackages(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdentical(recording, calling(map[string]string{"here": "call"}),
			calling(map[string]string{"there": "call"}), analysistest.TestData(), "twins/first", "twins/second")
	}),
		"the original found twins/first/twin.go:7:2: call, and the port did not",
		"the port found twins/second/twin.go:7:2: call, and the original did not",
	)
}

// The same in two folders of one fixture module: each file is named by its path below the module's folder, after the
// module's pattern, so the two never pair.
func TestRequireIdenticalTellsApartFilesOfOneNameInOneModule(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdentical(recording, calling(map[string]string{"here": "call"}),
			calling(map[string]string{"there": "call"}), analysistest.TestData(), "module_twins")
	}),
		"the original found module_twins/first/twin.go:7:2: call, and the port did not",
		"the port found module_twins/second/twin.go:7:2: call, and the original did not",
	)
}

// A differences fixture that is a module is read from its own folder, its records named as its findings are: they
// pair the port's findings, and a port that finds what the original does leaves each of them unmatched, named so.
func TestRequireOnlyAdjudicatedReadsAModulesRecords(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "module_adjudicated")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, calling(originalMessages), analysistest.TestData(),
			"module_adjudicated")
	}),
		"the fixture records that the port alone finds module_adjudicated/module_adjudicated.go:14:0: call to gained, "+
			"and it does not",
		"the fixture records that the port alone finds module_adjudicated/module_adjudicated.go:15:0: call to renamed "+
			"in the port's words, and it does not",
		"the fixture records that the original alone finds module_adjudicated/module_adjudicated.go:15:0: call to "+
			"renamed in the original's words, renamed, and it does",
	)
}

// A testdata that is a module may name its differences fixture by an import path its go.work resolves below src, as
// time_until's go1.8 testdata does: the records are read from the folder the load found the package in, so they pair
// the port's findings, and a port that finds what the original does leaves each of them unmatched. Read from where a
// layout would put the pattern, testdata/workspaced, they would be none, and the fixture would fail as recording
// nothing.
func TestRequireOnlyAdjudicatedReadsRecordsWhereTheLoadFoundThem(test *testing.T) {
	test.Parallel()
	workspace := filepath.Join(analysistest.TestData(), "module_workspace")
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, workspace, "workspaced")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, calling(originalMessages), workspace, "workspaced")
	}),
		"the fixture records that the port alone finds src/workspaced/workspaced.go:15:0: call to gained, and it "+
			"does not",
		"the fixture records that the port alone finds src/workspaced/workspaced.go:16:0: call to renamed in the "+
			"port's words, and it does not",
		"the fixture records that the original alone finds src/workspaced/workspaced.go:16:0: call to renamed in "+
			"the original's words, renamed, and it does",
	)
}
