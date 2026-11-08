package odd_slice_argument

import (
	"fmt"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/staticcheck/sa5012"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// staticcheck's own fixture for SA5012, copied unchanged from staticcheck/sa5012/testdata/go1.0/CheckEvenSliceLength
// into testdata/src/CheckEvenSliceLength, holds the port to every `//@ diag(...)` note the original expects, on the
// same line with a message the note matches, and to every `//@ fact(...)` note, the fact exported about the object it
// names on that line, printed as the note says, as staticcheck's testutil checks the original's; the port reports and
// exports nothing no note expects. The go1.0 folder sets the language version of staticcheck's run, which SA5012 never
// reads, so the fixture loads from the tree laid out by import path. Its strings.NewReplacer note holds the facts
// across packages, since the mark comes from the port's run on strings.
//
// This test is the port's own: parity.RequireStaticcheckWants reads diag notes alone and refuses a fixture that holds
// a fact note, so it runs the port over the fixture itself and compares both kinds through staticcheck_wants.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	wants, factWants, readError := staticcheck_wants.ReadWithFacts(filepath.Join(directory, "src",
		"CheckEvenSliceLength"))
	if readError != nil {
		test.Fatal(readError)
	}
	if len(wants) == 0 || len(factWants) == 0 {
		test.Fatal("the upstream fixture holds no diag or no fact notes, so the comparison proves nothing")
	}
	// The fixture is a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, []string{"CheckEvenSliceLength"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	for _, failure := range parity.LoadFailures(loaded.Roots) {
		test.Fatalf("fixture package %s does not load clean: %s", failure.Package, failure.Error)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{Analyzer}, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatal(analyzeError)
	}
	findings := []staticcheck_wants.Finding{}
	facts := []staticcheck_wants.FoundFact{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running on %s: %v", action.Package.ID, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			findings = append(findings, staticcheck_wants.Finding{
				File: position.Filename, Line: position.Line, Message: diagnostic.Message,
			})
		}
		objectFacts, _ := action.ExportedFacts()
		for _, objectFact := range objectFacts {
			position := action.Package.Fset.Position(objectFact.Object.Pos())
			facts = append(facts, staticcheck_wants.FoundFact{
				File: position.Filename, Line: position.Line, Object: objectFact.Object.Name(),
				Fact: fmt.Sprint(objectFact.Fact),
			})
		}
	}
	test.Logf("the fixture wants %d findings and %d facts, the port reported %d and exported %d", len(wants),
		len(factWants), len(findings), len(facts))
	unexpected, untaken := staticcheck_wants.Compare(wants, findings)
	for _, found := range unexpected {
		test.Errorf("%s:%d: the port reported %q, which no note on the line expects", filepath.Base(found.File),
			found.Line, found.Message)
	}
	for _, want := range untaken {
		test.Errorf("the port reported nothing matching %s", want)
	}
	unexpectedFacts, untakenFacts := staticcheck_wants.CompareFacts(factWants, facts)
	for _, found := range unexpectedFacts {
		test.Errorf("%s:%d: the port exported %q about %s, which no note on the line expects",
			filepath.Base(found.File), found.Line, found.Fact, found.Object)
	}
	for _, want := range untakenFacts {
		test.Errorf("the port exported nothing matching %s", want)
	}
}

// Over staticcheck's fixture and parity's packages, the original and the port find the same findings. parity holds
// every shape of the length check, either way around, checks that mark nothing because they do not dominate every
// return, do not panic, do not divide by two or do not read a length, chains of passing a slice on, sometimes and on
// every path, methods and a method expression, a function of another package, parity/pairs, and every way a slice's
// length is known or is not, with a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "CheckEvenSliceLength", "parity",
		"parity/pairs")
}

// Over the same fixtures, the marks the original and the port export on each fixture package are the same. The
// strings package's mark on NewReplacer's oldnew is not among them, since strings is no fixture package; the
// NewReplacer finding both tests above hold stands on it.
func TestPortExportsTheOriginalsFacts(test *testing.T) {
	test.Parallel()
	parity.RequireIdenticalFacts(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "CheckEvenSliceLength",
		"parity", "parity/pairs")
}

// The differences fixture holds the calls where the two could most plausibly part, a slice whose length depends on
// the path taken, one reaching the call through a loop's phi, arguments spread over lines and slices of slices of an
// array. It records no difference, since the port parts from the original nowhere it can run, so the two must agree
// there finding for finding and mark for mark.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "differences")
	parity.RequireIdenticalFacts(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// The one place the two part: a go or defer statement passing an odd slice to a marked parameter, whose call's
// source is the statement, which the original reads as a call expression and panics on, losing the package's every
// finding. The port reports each odd argument inside the statement's call, the two the fixture's go and defer pass
// and records, and not the even one.
func TestPortReportsWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	parity.RequirePortWhereOriginalPanics(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "statements")
}
