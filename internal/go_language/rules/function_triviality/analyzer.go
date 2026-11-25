// Package function_triviality answers whether a function of the package being analyzed is trivial, as
// intermediate_representation.IsTrivial judges it, through the run's fact resolver, so a rule whose finding rests on
// another function's body reads that body only through an answer the resolver records, and the engine learns which
// bodies the finding rested on.
//
// An answer is one walk of IsTrivial from the asked function, never made of other answers: the walk takes a callee met
// again as work, so what it answers of a callee depends on where the walk began, and only a walk begun at the asked
// function answers as a reader calling IsTrivial on it would. Beside the bit, an answer names every other top-level
// declaration whose body the walk read, each by the file a reader sees and the offset where the declaration starts,
// the key the engine holds a unit by, so an answer is known to stand until one of them or the asked function changes.
//
// A reader asks only of the functions and methods its own package declares (Result.IsTrivial). Another package's
// function has no syntax in the representation a reader walks, where IsTrivial refuses it at once, reading no body, so
// a reader takes it as not trivial without asking, and the walk refuses it the same where it does have its syntax.
package function_triviality

import (
	"fmt"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
)

// Analyzer hands the package's readers its triviality, which they ask of one function at a time. It requires ctrlflow,
// as the build of the package it judges does, so ctrlflow has run on the package, and the resolver holds it, before a
// reader asks.
var Analyzer = &analysis.Analyzer{
	Name:       "function_triviality",
	Doc:        "judges which of a package's functions are trivial when a reader asks, naming the bodies each answer read",
	Run:        run,
	Requires:   []*analysis.Analyzer{ctrlflow.Analyzer},
	ResultType: reflect.TypeFor[*Result](),
}

// run is the package's triviality, asked of the run's resolver as the package's readers ask.
func run(pass *analysis.Pass) (any, error) {
	resolver, resolverError := fact_resolver.Of(pass)
	if resolverError != nil {
		return nil, resolverError
	}
	reader := resolver.Package(pass.Pkg)
	if reader == nil {
		return nil, fmt.Errorf("reading the triviality of %s: the fact resolver never learned it, though ctrlflow, "+
			"which it learns a package from, ran on it first", pass.Pkg.Path())
	}
	return &Result{resolver: resolver, reader: reader}, nil
}
