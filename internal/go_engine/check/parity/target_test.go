package parity

import (
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// on386 is a 32-bit Linux target, the one each targeted fixture's narrow.go is built for, and onAmd64 a 64-bit one,
// which builds the targeted fixture's wide.go instead.
var (
	on386   = On("GOOS=linux", "GOARCH=386", "CGO_ENABLED=0")
	onAmd64 = On("GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
)

// Each target builds its own file of targeted and no other: for 386 both sides find the call in narrow.go, and for
// amd64 the original alone finds the one in wide.go.
func TestFindingsLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	originalFindings, portFindings := on386.Findings(test, original, port, analysistest.TestData(), "targeted")
	wanted := []Finding{{File: "targeted/narrow.go", Line: 6, Column: 2, Message: "call to shared"}}
	if !reflect.DeepEqual(originalFindings, wanted) || !reflect.DeepEqual(portFindings, wanted) {
		test.Errorf("for 386 the original found %v and the port %v, want %v from each", originalFindings,
			portFindings, wanted)
	}
	originalFindings, portFindings = onAmd64.Findings(test, original, port, analysistest.TestData(), "targeted")
	wantedOriginal := []Finding{{File: "targeted/wide.go", Line: 6, Column: 2, Message: "call to lost"}}
	if !reflect.DeepEqual(originalFindings, wantedOriginal) || len(portFindings) != 0 {
		test.Errorf("for amd64 the original found %v and the port %v, want %v and nothing", originalFindings,
			portFindings, wantedOriginal)
	}
}

// Each Require call that passes for 386 fails under the machine's own target, never 386 where these tests run, so
// what passes is the target's load: the two sides agree there and differ on the machine's own.
func TestRequireIdenticalLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireIdentical(recording, original, port, analysistest.TestData(), "targeted")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdentical(recording, original, port, analysistest.TestData(), "targeted")
	}), "the original found targeted/wide.go:6:2: call to lost, and the port did not")
}

// The note is on narrow.go, so the machine's own target builds no noted file.
func TestRequireStaticcheckWantsLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireStaticcheckWants(recording, port, analysistest.TestData(), "targeted")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckWants(recording, port, analysistest.TestData(), "targeted")
	}), "the fixtures hold no note, so the comparison proves nothing")
}

// The record is read whichever target builds its file, so on the machine's own target it is unmatched.
func TestRequireOnlyAdjudicatedLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "targeted_adjudicated")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, original, port, analysistest.TestData(), "targeted_adjudicated")
	}), "the fixture records that the port alone finds targeted_adjudicated/narrow.go:7:0: call to gained, and it "+
		"does not")
}

// The golden file is narrow.go's, so the machine's own target holds none.
func TestRequireStaticcheckGoldenLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireStaticcheckGolden(recording, keeping, analysistest.TestData(), "targeted_golden")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, keeping, analysistest.TestData(), "targeted_golden")
	}), "the fixtures hold no golden file, so the comparison proves nothing")
}

// The call the original panics on is in narrow.go, so on the machine's own target the original runs and the record
// there is unmatched.
func TestRequirePortWhereOriginalPanicsLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequirePortWhereOriginalPanics(recording, panicking(originalMessages), port, analysistest.TestData(),
			"targeted_panicking")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages), port, analysistest.TestData(),
			"targeted_panicking")
	}),
		"the original no longer panics on 1 of the 1 packages of targeted_panicking",
		"the fixture records that the port finds targeted_panicking/narrow.go:8:0: call to gained, and it does not",
	)
}

// A target that sets what a fixture load sets itself is refused, the tree's `GOPATH` and a module's `GOPROXY`, and so
// is a variable with no value.
func TestTargetRefusesAVariableTheLoadSets(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		On("GOPATH=/elsewhere").Findings(recording, original, port, analysistest.TestData(), "same")
	}), "the target sets GOPATH, which the load of [same] sets itself")
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		On("GOPROXY=direct").Findings(recording, original, port, analysistest.TestData(), "module_one")
	}), "the target sets GOPROXY, which the load of the module module_one sets itself")
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		On("GOARCH").Findings(recording, original, port, analysistest.TestData(), "same")
	}), `the target's "GOARCH" names no value`)
}
