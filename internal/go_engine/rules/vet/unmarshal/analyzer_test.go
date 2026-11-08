package unmarshal

import (
	"cmp"
	"go/types"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	upstream "golang.org/x/tools/go/analysis/passes/unmarshal"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for unmarshal, copied into testdata/src, hold the port to every want the original expects,
// and to nothing more. The copies are byte for byte but for typeparams.go, which is gofmt'd: upstream indents one line
// with spaces and ends without a newline, and gofmt changes only that line's indentation and the file's end, so every
// line stays where it was.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// Over x/tools' fixtures, parity's and dotted's, the last two holding every branch of the judgment and the shapes most
// likely to differ, the original and the port find the same findings, each the same in every field.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, upstream.Analyzer, Analyzer, analysistest.TestData(), "a", "typeparams", "parity",
		"dotted")
}

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour, each record with its reason: a Decode called as a method expression, whose value decoded into the port
// alone finds, and whose receiver the original alone wrongly finds.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, upstream.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// The original leaves the four encoding packages alone, in-package tests included, which share the package's path,
// and judges every other path, their external tests among them. parity's syntax and types are run under each path by
// passes built here, with a package of that path in place of parity's: both analyzers read the package only for its
// path, and the callees they judge from the types are the real encoding packages', as in the encoding packages
// themselves. A path judged must find what parity finds under its own path, on both sides, and a path left alone
// nothing, so the comparison cannot pass on two empty sides.
//
// The harness has no form for this: it runs each fixture under the path it loads with, and a fixture cannot load as
// encoding/json, a path the standard library already holds. So the passes are built here, and the harness's Finding
// and Difference compare what they find.
func TestPortLeavesTheEncodingPackagesAloneAsTheOriginalDoes(test *testing.T) {
	test.Parallel()
	fixture := loadedFixture(test, "parity")
	original := findingsUnder(test, upstream.Analyzer, fixture, fixture.PkgPath)
	port := findingsUnder(test, Analyzer, fixture, fixture.PkgPath)
	test.Logf("under %s the original found %d, the port %d", fixture.PkgPath, len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in parity, so the comparison proves nothing")
	}
	leftAlone := []string{"encoding/gob", "encoding/json", "encoding/xml", "encoding/asn1"}
	judged := []string{
		"encoding/json_test", "encoding/gob_test", "encoding/xml_test", "encoding/asn1_test", "encoding/json.test",
		"encoding", "encoding/base64", "encoding/jsonv2", "example.com/encoding/json",
	}
	for _, path := range slices.Concat(leftAlone, judged) {
		originalUnder := findingsUnder(test, upstream.Analyzer, fixture, path)
		portUnder := findingsUnder(test, Analyzer, fixture, path)
		expected := original
		if slices.Contains(leftAlone, path) {
			expected = []parity.Finding{}
		}
		for side, found := range map[string][]parity.Finding{"the original": originalUnder, "the port": portUnder} {
			if len(parity.Difference(found, expected)) > 0 || len(parity.Difference(expected, found)) > 0 {
				test.Errorf("under %s %s found %d, expected %d", path, side, len(found), len(expected))
			}
		}
	}
}

// loadedFixture loads the one fixture package pattern names, which must load clean.
func loadedFixture(test *testing.T, pattern string) *packages.Package {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, []string{pattern}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %s: %v", pattern, loadError)
	}
	if len(loaded.Roots) != 1 {
		test.Fatalf("loading %s found %d packages, expected 1", pattern, len(loaded.Roots))
	}
	// A fixture with one type error would leave its findings uncounted, and the comparison would read green.
	for _, failure := range parity.LoadFailures(loaded.Roots) {
		test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %s",
			failure.Package, failure.Error)
	}
	return loaded.Roots[0]
}

// findingsUnder runs analyzer over fixture's syntax and types alone, through passes whose package has path in place
// of fixture's own, and returns its findings, sorted. Everything analyzer requires runs first over the same package,
// as a driver runs it: the original's inspector, and the port's syntax dispatcher with what that walk reads, the
// dispatcher's analyzer itself reading the package's path where the port's listener does.
func findingsUnder(
	test *testing.T,
	analyzer *analysis.Analyzer,
	fixture *packages.Package,
	path string,
) []parity.Finding {
	test.Helper()
	found := []parity.Finding{}
	underPath := types.NewPackage(path, fixture.Types.Name())
	results := map[*analysis.Analyzer]any{}
	var run func(running *analysis.Analyzer, report func(diagnostic analysis.Diagnostic))
	run = func(running *analysis.Analyzer, report func(diagnostic analysis.Diagnostic)) {
		resultOf := map[*analysis.Analyzer]any{}
		for _, required := range running.Requires {
			if _, isRun := results[required]; !isRun {
				run(required, func(analysis.Diagnostic) {
					// A requirement's findings are not the analyzer's, and none of these makes any.
				})
			}
			resultOf[required] = results[required]
		}
		pass := &analysis.Pass{
			Analyzer:   running,
			Fset:       fixture.Fset,
			Files:      fixture.Syntax,
			Pkg:        underPath,
			TypesInfo:  fixture.TypesInfo,
			TypesSizes: fixture.TypesSizes,
			ResultOf:   resultOf,
			Report:     report,
		}
		result, runError := running.Run(pass)
		if runError != nil {
			test.Fatalf("running %s under %s: %v", running.Name, path, runError)
		}
		results[running] = result
	}
	run(analyzer, func(diagnostic analysis.Diagnostic) {
		found = append(found, findingOf(fixture, diagnostic))
	})
	slices.SortFunc(found, func(first parity.Finding, second parity.Finding) int {
		return cmp.Compare(first.String(), second.String())
	})
	return found
}

// findingOf is diagnostic, reported in fixture, as the harness compares it, in its file's base name. Neither side
// relates a position or suggests a fix, so the range and the message are the whole of it.
func findingOf(fixture *packages.Package, diagnostic analysis.Diagnostic) parity.Finding {
	position := fixture.Fset.Position(diagnostic.Pos)
	finding := parity.Finding{
		File:    filepath.Base(position.Filename),
		Line:    position.Line,
		Column:  position.Column,
		Message: diagnostic.Message,
	}
	// The original reports at a position with no end, so the end is zero on both sides, and comparing it still holds
	// the port to reporting none.
	if diagnostic.End.IsValid() {
		end := fixture.Fset.Position(diagnostic.End)
		finding.EndLine = end.Line
		finding.EndColumn = end.Column
	}
	return finding
}
