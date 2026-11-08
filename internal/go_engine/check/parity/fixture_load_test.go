package parity

import (
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// A folder of testdata holding a go.mod loads as a module, from its folder: module_one imports its package inner by
// the module's path, which resolves nowhere in the tree under src, and inner's call is found too. Its files are named
// after the module's pattern, and the tree's beside them by their base names.
func TestFindingsLoadsAFolderHoldingAGoModAsAModule(test *testing.T) {
	test.Parallel()
	_, portFindings := Findings(test, original, port, analysistest.TestData(), "same", "module_one")
	wantedPort := []Finding{
		{File: "module_one/inner.go", Line: 8, Column: 2, Message: "call to shared"},
		{File: "module_one/module_one.go", Line: 14, Column: 2, Message: "call to shared"},
		{File: "module_one/module_one.go", Line: 15, Column: 2, Message: "call to gained"},
		{File: "same.go", Line: 7, Column: 2, Message: "call to shared"},
		{File: "same.go", Line: 8, Column: 2, Message: "call to shared"},
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
