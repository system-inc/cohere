package impossible_target_comparison

import (
	"strings"
	"testing"

	"honnef.co/go/tools/staticcheck/sa4032"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// targets are the targets every fixture is loaded for, each on its own, since the go tool loads only the files a
// target builds, and the fixtures' files are constrained to linux, windows, unix and others: each file builds on one
// of these at least, plan9 being neither unix nor windows. Cgo is off, as it is by default for a target other than the
// machine's, and no fixture needs it.
var targets = [][]string{
	{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"},
	{"GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0"},
	{"GOOS=plan9", "GOARCH=amd64", "CGO_ENABLED=0"},
}

// targetName is a target as its subtest is named, its variables joined, as "GOOS=linux,GOARCH=amd64,CGO_ENABLED=0".
func targetName(target []string) string {
	return strings.Join(target, ",")
}

// staticcheck's own fixture for SA4032, copied unchanged from its testdata/go1.0 into testdata/src: every
// `//@ diag(...)` note the original expects, the port reports, and it reports nothing no note expects. The fixture
// uses nothing newer than Go 1.0, so it needs no version of its own. Its files are constrained to linux, windows, unix
// and neither, and staticcheck's own run reads those its host builds; this one loads it for each of targets, each held
// to the notes of the files it builds, so every file and every note is read.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, target := range targets {
		test.Run(targetName(target), func(test *testing.T) {
			test.Parallel()
			parity.On(target...).RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckGOOSComparison")
		})
	}
}

// Over staticcheck's fixture and parity's, the second holding the shapes the check depends on in files constrained to
// each target, a dot import and a generated file, the original and the port find the same findings on each of
// targets.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	for _, target := range targets {
		test.Run(targetName(target), func(test *testing.T) {
			test.Parallel()
			parity.On(target...).RequireIdentical(test, sa4032.Analyzer, Analyzer, analysistest.TestData(),
				"CheckGOOSComparison", "parity")
		})
	}
}

// The differences fixture holds the cases where the port and the original could plausibly disagree, and records
// nothing, because the two read the same form and differ nowhere, so the two must find the same findings there. Its
// files with comparisons build for linux, differences.go on amd64 alone, so it loads for linux on amd64, the first of
// targets; for the others only use.go builds, which compares nothing, and neither side finds anything.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.On(targets[0]...).RequireIdentical(test, sa4032.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
