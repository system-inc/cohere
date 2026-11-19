package odd_slice_argument

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// ownMarksResult is every parameter the analyzer's own marks hold in the package it runs on.
type ownMarksResult map[types.Object]bool

// ownMarksAnalyzer hands the tests the marks the analyzer makes of the package it runs on, which run keeps only while
// it reports. It makes them through ownMarks, the function run makes them through, on the same functions in the same
// order, asking the same resolver, so what the definition's oracle holds to the definition is what run reports by.
var ownMarksAnalyzer = &analysis.Analyzer{
	Name:       "odd_slice_argument_own_marks",
	Doc:        "the marks SA5012 makes of the package it runs on, as its run makes them",
	Requires:   []*analysis.Analyzer{build_intermediate_representation.Analyzer},
	ResultType: reflect.TypeFor[ownMarksResult](),
	Run: func(pass *analysis.Pass) (any, error) {
		built, readError := build_intermediate_representation.ResultOf(pass)
		if readError != nil {
			return nil, readError
		}
		resolver, resolverError := fact_resolver.Of(pass)
		if resolverError != nil {
			return nil, resolverError
		}
		reader := resolver.Package(pass.Pkg)
		if reader == nil {
			return nil, fmt.Errorf("reading the marks of %s: the fact resolver never learned it", pass.Pkg.Path())
		}
		functions := slices.Collect(built.SourceFunctions())
		return ownMarksResult(ownMarks(resolver, reader, functions, pass.Analyzer.Name).recorded), nil
	},
}
