// Package build_intermediate_representation builds the engine's own intermediate representation of a package, the
// one in internal/go_engine/intermediate_representation, for the readers that move onto it from staticcheck's
// (#g0rwdw1). It stands where staticcheck's buildir stands for go/ir: the program and package are created from the
// pass's types and syntax with the no-return predicate buildir gives go/ir, and Result lists the source functions in
// buildir's order. Unlike buildir it builds nothing as it runs: Result builds the whole package on its first read, so
// a package nobody reads costs only its creation.
//
// It is named for what it does, as buildir and x/tools' buildssa are, rather than go_ir: every comment in this tree
// says go/ir for staticcheck's package, which this replaces.
package build_intermediate_representation

import (
	"fmt"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Analyzer creates the pass's package in our intermediate representation, built whole when its result is first read.
// It requires ctrlflow, as buildir does, for the same no-return predicate, so where both run the two representations
// end the same calls in the same `Panic`.
var Analyzer = &analysis.Analyzer{
	Name: "build_intermediate_representation",
	Doc: "creates the package in the engine's own intermediate representation, built whole when a reader first " +
		"reads it, for the readers that later passes are",
	Run:        run,
	ResultType: reflect.TypeFor[*Result](),
	Requires:   []*analysis.Analyzer{ctrlflow.Analyzer},
}

// ExpressionReader is what an analyzer requires, beside Analyzer itself, to read Function.ValueForExpression of the
// package Analyzer builds. Analyzer records the expression map for a package exactly when an analyzer requiring
// ExpressionReader requires Analyzer there, which it reads from the set of its dependents the engine's checker hands
// each pass under rule.Dependents. A rule's analyzer runs only on the packages a run checks, and only when the run
// enables the rule, so the workspace's packages get the map where a reader of it is on, and a dependency, built for
// facts, never does.
//
// Under a driver that hands no such set, analysistest's or a test calling checker.Analyze without
// Options.Dependents, every package records it, as buildir's GlobalDebug records it for every package.
//
// It is a mark, read from the requiring analyzer's Requires, and its own run finds nothing. It runs because a mark
// the checker never ran would need the checker to know of it, where this one needs nothing from the checker.
var ExpressionReader = &analysis.Analyzer{
	Name: "expression_reader",
	Doc: "marks the analyzer that requires it as a reader of the expression map, so the intermediate " +
		"representation records it on that analyzer's packages; it finds nothing",
	RunDespiteErrors: true,
	Run: func(*analysis.Pass) (any, error) {
		return nil, nil
	},
}

// run creates a program for the pass's package alone, as buildir does: one per package, since the analysis API has no
// place to hang a program every package shares. Each direct import is created from its types alone, so a call into it
// finds its callee, and the package from its syntax, its expression map on when a reader of the map requires Analyzer
// on it.
func run(pass *analysis.Pass) (any, error) {
	graphs, isGraphs := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
	if !isGraphs {
		return nil, fmt.Errorf("reading %s's result: it is %T, where ctrlflow's graphs were expected",
			ctrlflow.Analyzer.Name, pass.ResultOf[ctrlflow.Analyzer])
	}
	program := intermediate_representation.NewProgram(pass.Fset)
	program.SetNoReturn(graphs.NoReturn)
	for _, imported := range pass.Pkg.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	created := program.CreatePackage(pass.Pkg, pass.Files, pass.TypesInfo, false)
	created.SetDebugMode(readsExpressions(pass))
	return &Result{irPackage: created}, nil
}

// readsExpressions is whether an analyzer that reads the expression map requires Analyzer on the pass's package, or
// true where the driver doesn't say which analyzers do (see ExpressionReader).
func readsExpressions(pass *analysis.Pass) bool {
	dependents, knowsDependents := pass.ResultOf[rule.Dependents].(map[*analysis.Analyzer]bool)
	if !knowsDependents {
		return true
	}
	for dependent := range dependents {
		if slices.Contains(dependent.Requires, ExpressionReader) {
			return true
		}
	}
	return false
}
