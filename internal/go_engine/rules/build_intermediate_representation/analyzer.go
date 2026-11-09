// Package build_intermediate_representation builds the engine's own intermediate representation of a package, the
// one in internal/go_engine/intermediate_representation, for the readers that move onto it from staticcheck's
// (#g0rwdw1). It stands where staticcheck's buildir stands for go/ir: the program and package are created from the
// pass's types and syntax with the no-return predicate buildir gives go/ir, and Result lists the source functions in
// buildir's order. As buildir does, it builds the whole package as it runs, an action of its own in the run's graph
// that the checker schedules as soon as ctrlflow finishes there, so every reader of the package starts on it built
// and none waits inside its own action on another's build (#zrfcwdy). It runs only where a reader requires it, and
// every reader reads the whole package, so building before any of them asks costs nothing they would not.
//
// It is named for what it does, as buildir and x/tools' buildssa are, rather than go_ir: every comment in this tree
// says go/ir for staticcheck's package, which this replaces.
package build_intermediate_representation

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Analyzer creates the pass's package in our intermediate representation and builds it whole.
// It requires ctrlflow, as buildir does, for the same no-return predicate, so where both run the two representations
// end the same calls in the same `Panic`.
var Analyzer = &analysis.Analyzer{
	Name: "build_intermediate_representation",
	Doc: "creates and builds the package in the engine's own intermediate representation, for the readers that " +
		"later passes are",
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

// Sharer is what the engine's checker hands every pass under rule.Resolved, the run's fact resolver, which builds the
// functions of a package it judges in the representation the readers walk, so a root's is built once in a run, not
// once here and once again for the facts the run's entries hold (#wt5vaft). ReadersPackage is the package whose types
// are packageTypes as NewPackage creates it for the readers, created on the first call, by the resolver's first
// question there or by this analyzer's run, whichever comes first, or nil for a package the resolver learned no
// action of this analyzer on, which run then creates alone.
type Sharer interface {
	ReadersPackage(packageTypes *types.Package) *intermediate_representation.Package
}

// run is the pass's package as the readers walk it, built: the one the run's resolver shares, created as NewPackage
// creates it, or, under a driver with no resolver, NewPackage's own. Either way its expression map is on when a reader
// of the map requires Analyzer on it, and a package the resolver created before this ran, for a question that reached
// it first, was created so too: the resolver read the same set of dependents, from the graph, before any action ran.
// A function the resolver built already is not built again: Package.Build waits on each function's own build.
func run(pass *analysis.Pass) (any, error) {
	graphs, isGraphs := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
	if !isGraphs {
		return nil, fmt.Errorf("reading %s's result: it is %T, where ctrlflow's graphs were expected",
			ctrlflow.Analyzer.Name, pass.ResultOf[ctrlflow.Analyzer])
	}
	dependents, knowsDependents := pass.ResultOf[rule.Dependents].(map[*analysis.Analyzer]bool)
	readsExpressions := ReadsExpressions(dependents, knowsDependents)
	if sharer, shares := pass.ResultOf[rule.Resolved].(Sharer); shares {
		if shared := sharer.ReadersPackage(pass.Pkg); shared != nil {
			if shared.RecordsExpressions() != readsExpressions {
				// A reader of the map would find none, or the package would record one nobody reads; the resolver
				// reads the same dependents this pass holds, so the two never part.
				return nil, fmt.Errorf("the fact resolver shares %s recording expressions %t, where this pass's "+
					"dependents ask %t", pass.Pkg.Path(), shared.RecordsExpressions(), readsExpressions)
			}
			shared.Build()
			return &Result{irPackage: shared}, nil
		}
	}
	created := NewPackage(pass.Fset, pass.Pkg, pass.Files, pass.TypesInfo, graphs.NoReturn, readsExpressions)
	created.Build()
	return &Result{irPackage: created}, nil
}

// NewPackage creates packageTypes's package in a program of its own, as buildir does: one per package, since the
// analysis API has no place to hang a program every package shares. The program's no-return predicate is noReturn,
// ctrlflow's on the package. Each direct import is created from its types alone, so a call into it finds its callee,
// and the package from its syntax, files and info, its expression map on when readsExpressions is set. Nothing is
// built yet.
func NewPackage(
	fileSet *token.FileSet,
	packageTypes *types.Package,
	files []*ast.File,
	info *types.Info,
	noReturn func(*types.Func) bool,
	readsExpressions bool,
) *intermediate_representation.Package {
	program := intermediate_representation.NewProgram(fileSet)
	program.SetNoReturn(noReturn)
	for _, imported := range packageTypes.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	created := program.CreatePackage(packageTypes, files, info, false)
	created.SetDebugMode(readsExpressions)
	return created
}

// ReadsExpressions is whether an analyzer that reads the expression map is among dependents, the analyzers that
// require Analyzer on a package, or true where the driver doesn't say which analyzers do, knowsDependents unset (see
// ExpressionReader).
func ReadsExpressions(dependents map[*analysis.Analyzer]bool, knowsDependents bool) bool {
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
