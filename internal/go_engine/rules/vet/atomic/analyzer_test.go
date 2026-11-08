package atomic

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/atomic"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for atomic, copied unchanged into testdata/src, hold the port to every want the original
// expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// Parity's wants are what the original makes of it, so each line's comment, reported or clean, says truly what both
// sides do there, and a shape the differential passes is a shape whose outcome was read, not only matched. The
// original stands where RequireUpstreamWants takes a port, since the run holds whichever analyzer it is given to the
// fixture's wants.
func TestParityWantsAreTheOriginals(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, atomic.Analyzer, analysistest.TestData(), "parity")
}

// upstreamLoadErrors are the type errors x/tools' fixtures load with, since atomic runs despite errors and its
// fixtures hold calls that do not type-check: a's AddUint64 with no arguments, which used to crash vet, and in
// typeparams the Add calls on a type parameter, which its own comment says type-check wrong on purpose, and the
// generic _S named as a receiver without its type parameter, which upstream leaves unremarked.
var upstreamLoadErrors = []parity.LoadError{
	{
		Position: "a/a.go:43:23",
		Message:  "not enough arguments in call to atomic.AddUint64 have () want (*uint64, uint64)",
	},
	{
		Position: "typeparams/typeparams.go:19:10",
		Message:  "cannot use atomic.AddInt64(addr, -delta) (value of type int64) as T value in assignment",
	},
	{
		Position: "typeparams/typeparams.go:19:26",
		Message:  "cannot use addr (variable of type *T) as *int64 value in argument to atomic.AddInt64",
	},
	{
		Position: "typeparams/typeparams.go:19:32",
		Message: "cannot use -delta (value of type T constrained by Subtractable) as int64 value in argument to " +
			"atomic.AddInt64",
	},
	{
		Position: "typeparams/typeparams.go:20:10",
		Message:  "cannot use atomic.AddUintptr(addr, delta) (value of type uintptr) as T value in assignment",
	},
	{
		Position: "typeparams/typeparams.go:20:28",
		Message:  "cannot use addr (variable of type *T) as *uintptr value in argument to atomic.AddUintptr",
	},
	{
		Position: "typeparams/typeparams.go:20:34",
		Message: "cannot use delta (variable of type T constrained by Subtractable) as uintptr value in argument to " +
			"atomic.AddUintptr",
	},
	{
		Position: "typeparams/typeparams.go:21:18",
		Message:  "not enough arguments in call to atomic.AddInt64 have () want (*int64, int64)",
	},
	{
		Position: "typeparams/typeparams.go:29:9",
		Message:  "cannot use generic type _S[T Subtractable] without instantiation",
	},
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field, x/tools' compared despite
// the type errors upstreamLoadErrors lists and parity's loading clean. The port differs from the original nowhere, so
// there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdenticalDespiteLoadErrors(test, atomic.Analyzer, Analyzer, analysistest.TestData(),
		upstreamLoadErrors, "a", "typeparams", "parity")
}
