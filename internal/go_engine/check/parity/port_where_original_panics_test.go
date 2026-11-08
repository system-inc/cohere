package parity

import (
	"errors"
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// panicking is an original that panics on a package holding a call to panics, as staticcheck's SA1001 panics on a
// call its assertion refuses, before it reports anything, and elsewhere reports each call messages names, as calling
// does.
func panicking(messages map[string]string) *analysis.Analyzer {
	reporting := calling(messages)
	return &analysis.Analyzer{
		Name: "panicking",
		Doc:  "panicking panics on a call to panics.",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					call, isCall := node.(*ast.CallExpr)
					if !isCall {
						return true
					}
					if identifier, isIdentifier := call.Fun.(*ast.Ident); isIdentifier && identifier.Name == "panics" {
						panic("a call to panics")
					}
					return true
				})
			}
			return reporting.Run(pass)
		},
	}
}

// failing is an analyzer that fails on every package with an error of its own, not a panic.
var failing = &analysis.Analyzer{
	Name: "failing",
	Doc:  "failing fails on every package.",
	Run: func(*analysis.Pass) (any, error) {
		return nil, errors.New("a planted failure")
	},
}

// The port finds exactly what the fixture records, where the original panics.
func TestRequirePortWhereOriginalPanicsPassesWhatTheFixtureRecords(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages), port, analysistest.TestData(),
			"panicking")
	}))
}

// A port that misses a recorded finding fails on that record.
func TestRequirePortWhereOriginalPanicsFailsAMissingFinding(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages), calling(sharedMessages),
			analysistest.TestData(), "panicking")
	}), "the fixture records that the port finds panicking/panicking.go:14:0: call to gained, and it does not")
}

// A port that finds what no record holds fails on that finding.
func TestRequirePortWhereOriginalPanicsFailsAnExtraFinding(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages), calling(map[string]string{
			"panics": "call to panics", "shared": "call to shared", "gained": "call to gained",
		}), analysistest.TestData(), "panicking")
	}), "the port found panicking/panicking.go:12:0: call to panics, which the fixture does not record")
}

// An original that runs over the fixture no longer panics there, and the fixture belongs to a comparison.
func TestRequirePortWhereOriginalPanicsFailsAnOriginalThatRuns(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, original, port, analysistest.TestData(), "panicking")
	}), "the original no longer panics on 1 of the 1 packages of panicking, so the fixture belongs to "+
		"RequireIdentical or RequireOnlyAdjudicated now")
}

// An original that fails with an error of its own has not panicked.
func TestRequirePortWhereOriginalPanicsFailsAnOriginalThatFailsOtherwise(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, failing, port, analysistest.TestData(), "panicking")
	}), "the original failed on panicking other than by panicking: a planted failure")
}

// A port that does not run fails, since its silence would read as meeting no record.
func TestRequirePortWhereOriginalPanicsFailsAPortThatDoesNotRun(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages), failing, analysistest.TestData(),
			"panicking")
	}), "running the port on panicking: a planted failure")
}

func TestRequirePortWhereOriginalPanicsFailsAFixtureThatRecordsNothing(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages), calling(sharedMessages),
			analysistest.TestData(), "panicking_silent")
	}), "the fixture panicking_silent records nothing, so the comparison proves nothing")
}

// A record with no comment above it and no doc comment on the function it is in fails, as RequireOnlyAdjudicated
// fails it.
func TestRequirePortWhereOriginalPanicsFailsARecordWithNoReason(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages), port, analysistest.TestData(),
			"panicking_unreasoned")
	}), "panicking_unreasoned/panicking_unreasoned.go:10 holds a record with no reason")
}

// A record of any kind but `finds:` is refused, since the original makes no finding there for it to cover.
func TestRequirePortWhereOriginalPanicsRefusesARecordOtherThanFinds(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages), port, analysistest.TestData(),
			"panicking_renamed")
	}), "panicking_renamed/panicking_renamed.go:11 records `renames:`, and where the original panics it finds "+
		"nothing for the record to cover")
}
