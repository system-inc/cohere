package shift

import (
	"os"
	"testing"

	"golang.org/x/tools/go/analysis"
	upstream "golang.org/x/tools/go/analysis/passes/shift" // The original, named apart from the port's package.

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// x/tools' own fixtures for shift, copied into testdata/src, hold the port to every want the original expects, and to
// nothing more. a/a.go is copied unchanged. typeparams/typeparams.go is gofmt'd, as every Go file cohere owns must be,
// which changed the spacing within six of its lines and moved none, so its wants name the lines they named.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and of the dead code detector,
// type parameters with one width and with several, and the shapes most likely to differ, the original and the port
// find the same findings, each the same in every field. The end matters here, since both report the whole shift's
// range, and a port that reported the shifted operand's range alone would start at the same place. The port differs
// from the original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, upstream.Analyzer, Analyzer, analysistest.TestData(), "a", "typeparams", "parity")
}

// BenchmarkOverTheCheckPackage weighs the port over the engine's check package, a real package of the repository
// and one of its largest, so what the port gathers and keeps over a package's walk is weighed at a size the engine
// meets. The package is loaded once and every prerequisite run once on it, so each iteration is the dispatcher's walk
// with the port's listener alone, and the allocations reported are the walk's and the port's. It is skipped under
// -short.
func BenchmarkOverTheCheckPackage(benchmark *testing.B) {
	if testing.Short() {
		benchmark.Skip("loads a package of the repository")
	}
	loaded, loadError := load.Packages(".", []string{"github.com/system-inc/cohere/internal/go_engine/check"}, nil, nil)
	if loadError != nil {
		benchmark.Fatal(loadError)
	}
	// A dispatcher names each rule once, and the shared one already holds the port, so the benchmark's is new.
	dispatcher := dispatch.NewSyntax()
	newAnalyzer(dispatcher)
	passes := []*analysis.Pass{}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			benchmark.Fatalf("package %s does not load clean: %v", root.ID, root.Errors)
		}
		results := map[*analysis.Analyzer]any{}
		passOf := func(analyzer *analysis.Analyzer) *analysis.Pass {
			return &analysis.Pass{
				Analyzer: analyzer, Fset: root.Fset, Files: root.Syntax, Pkg: root.Types, TypesInfo: root.TypesInfo,
				TypesSizes: root.TypesSizes, ResultOf: results, ReadFile: os.ReadFile,
				Report: func(analysis.Diagnostic) {
					// The findings are the tests' to compare; this weighs only what finding them costs.
				},
			}
		}
		// Every prerequisite runs once, in the order its own requirements need, before the walk is weighed.
		var prepare func(analyzer *analysis.Analyzer)
		prepare = func(analyzer *analysis.Analyzer) {
			for _, required := range analyzer.Requires {
				if _, prepared := results[required]; !prepared {
					prepare(required)
					result, runError := required.Run(passOf(required))
					if runError != nil {
						benchmark.Fatalf("running %s on %s: %v", required.Name, root.ID, runError)
					}
					results[required] = result
				}
			}
		}
		prepare(dispatcher.Analyzer())
		passes = append(passes, passOf(dispatcher.Analyzer()))
	}
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		for _, pass := range passes {
			if _, runError := dispatcher.Analyzer().Run(pass); runError != nil {
				benchmark.Fatal(runError)
			}
		}
	}
}
