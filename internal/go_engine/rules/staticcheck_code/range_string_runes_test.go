package staticcheck_code_test

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/simple/s1029"
	"honnef.co/go/tools/staticcheck/sa6003"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// rangeStringRunesAnalyzer runs the port over every function of a package, as SA6003's and S1029's ports will, a
// plain analyzer over staticcheck_ir.Functions with the context staticcheck_report.PassContext builds around
// pass.Report, so the test reads the port's findings straight off it, outside the engine's dispatcher.
var rangeStringRunesAnalyzer = &analysis.Analyzer{
	Name:     "RangeStringRunes",
	Doc:      "runs staticcheck_code.RangeStringRunes over every function, to compare with staticcheck's",
	Requires: []*analysis.Analyzer{staticcheck_ir.Analyzer, generated.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		functions, readError := staticcheck_ir.Functions(pass)
		if readError != nil {
			return nil, readError
		}
		context, contextError := staticcheck_report.PassContext(pass)
		if contextError != nil {
			return nil, contextError
		}
		for _, function := range functions {
			staticcheck_code.RangeStringRunes(context, function)
		}
		return nil, nil
	},
}

// The original is internal to staticcheck, so it runs here as the two checks that run it, SA6003 and S1029, beside
// the port over the range_string_runes fixture, which holds every way the conversion, the slice's readers and the
// range's place could make the judgment go either way. The port must report what each of them reports, start, end
// and message, as a multiset, and the test fails if they report nothing.
func TestRangeStringRunesMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"range_string_runes"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading range_string_runes: %v", loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		// A fixture that does not type-check is one every side skips, and would compare nothing.
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{sa6003.Analyzer, s1029.Analyzer, rangeStringRunesAnalyzer}, loaded.Roots,
		&checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing range_string_runes: %v", analyzeError)
	}
	found := map[*analysis.Analyzer][]string{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			end := action.Package.Fset.Position(diagnostic.End)
			found[action.Analyzer] = append(found[action.Analyzer], fmt.Sprintf("%s:%d:%d-%d:%d: %s",
				filepath.Base(position.Filename), position.Line, position.Column, end.Line, end.Column,
				diagnostic.Message))
		}
	}
	port := found[rangeStringRunesAnalyzer]
	slices.SortFunc(port, cmp.Compare[string])
	for _, original := range []*analysis.Analyzer{sa6003.Analyzer, s1029.Analyzer} {
		upstream := found[original]
		slices.SortFunc(upstream, cmp.Compare[string])
		test.Logf("%s reported %d, the port %d", original.Name, len(upstream), len(port))
		if len(upstream) == 0 {
			test.Fatalf("%s reported nothing in the fixture, so the comparison proves nothing", original.Name)
		}
		if !slices.Equal(upstream, port) {
			test.Errorf("%s reported\n%v\nand the port\n%v", original.Name, upstream, port)
		}
	}
}
