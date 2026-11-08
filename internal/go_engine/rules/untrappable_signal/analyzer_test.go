package untrappable_signal

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1016"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA1016, copied unchanged from
// staticcheck/sa1016/testdata/go1.0/CheckUntrappableSignal into testdata/src/CheckUntrappableSignal with both its
// files and their golden files; the second carries its `//go:build` line beside the old `// +build` one, so gofmt
// leaves it as it is. The go1.0 folder sets the language version of staticcheck's run, which SA1016 never reads, so
// the fixture loads from a tree laid out by import path as the house's fixtures do, on a machine whose system the
// second file's constraint names.
const upstream = "CheckUntrappableSignal"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to staticcheck's fixture, make its golden files, as staticcheck's testutil requires of the
// original's: one whose golden file has a section for each of four fixes and one whose golden file is a single
// comment.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, fixes included. parity
// holds every way the original accepts or refuses a signal: each of the three functions called directly, through
// parentheses, a renamed or dot import and a variable, each untrappable signal plain, converted to os.Signal and
// parenthesized, a call that already traps `syscall.SIGTERM`, which loses the fix that would add it, signals named
// through a dot import, several in one call, a local value spelled like syscall, NotifyContext and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1016.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the calls where walking every call in place of the type index's calls could part,
// and where the fixes' printing matters, a call spread over lines, a variadic call and calls through parentheses. It
// records no difference, since there is none, so the two must agree there finding for finding, fixes included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1016.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
