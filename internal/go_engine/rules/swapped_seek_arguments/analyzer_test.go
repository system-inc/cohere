package swapped_seek_arguments

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1013"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA1013, copied unchanged from staticcheck/sa1013/testdata/go1.0/checkStdlibUsageSeeker
// into testdata/src/checkStdlibUsageSeeker with its golden file, holds the port to every `//@ diag(...)` note the
// original expects, and to nothing more. The go1.0 folder sets the language version of staticcheck's run, which
// SA1013 never reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "checkStdlibUsageSeeker")
}

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), "checkStdlibUsageSeeker")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, fixes included. parity
// holds every whence constant given first, through interfaces, pointers, embedded structs and interfaces, generic
// types and a type parameter, a renamed import, parentheses around the function and the arguments, arguments across
// lines, one call nested in another's arguments, calls under go, defer and a closure, and the calls the original
// leaves alone, a swapped signature, a named whence type, a field holding a function, a struct field spelled
// SeekStart, a method expression, a method value called later, a conversion and a constant expression, and a
// generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1013.Analyzer, Analyzer, analysistest.TestData(), "checkStdlibUsageSeeker",
		"parity")
}

// The differences fixture holds the shapes where the two could most plausibly part, a dot import beside the same
// package imported under a name, a Seek returning a named int64, and arguments the fix writes across lines and with a
// comment among them. It records no difference, since there is none, so the two must agree there finding for
// finding, fixes included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1013.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
