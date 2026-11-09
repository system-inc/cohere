// Package return_nil_ness judges whether each function's results may be nil, staticcheck's nilness facts ported onto
// the engine's own intermediate representation, so the readers asking what a function returns read them without
// buildir (#w7zmvat).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 analysis/facts/nilness/nilness.go, whose algorithm it
// keeps step for step: the package's source functions judged in the order buildir lists them, with one set of the
// functions whose judging has begun shared by them all, a callee of the same package judged as it is met, a callee
// already begun, as in a cycle of calls, answering the defaults of its signature, and each function's results judged
// by a forward data flow analysis over its blocks, forward_flow's port of staticcheck's dense.Forward, on staticcheck's
// own dfa lattices. The original pushes its facts: it judges every function of every package, dependencies included,
// and marks each whose results say more than that each may be nil with a fact its importers read. Here a reader asks
// instead (#w7zmvat unit 2): Result.Nilness judges a function only when asked, through the run's fact resolver,
// building only what that judgment reaches, and gives every answer the push gives. demand_walk.go says how; the push
// walk stays in push_walk.go, for the resolver's fallback and the tests.
//
// It differs in one place, adjudicated: how a block's phis are set. Staticcheck's processPhis sets them one after
// another, so a phi whose edge is a sibling phi of the block reads the sibling's new value when the sibling stands
// first and its old value when it stands after, an answer hanging on the order lift.go places them in. The port reads
// every edge before it sets any phi (see transferPhis), which is what a phi means and cannot hang on the order, the
// more so since our phis are placed by Braun's construction, in another order than go/ir's. The differences fixture
// holds a loop where staticcheck's order reads the new value and loses a fact the port keeps, each side's answer
// recorded in TestPortDiffersFromOriginalOnlyWhereItIsRight, and holds the port to the same fact in either order. Over
// the four trees the fact differential runs on, the two export the same facts: root 8387, the standard library 14089,
// loom 2133 and deeafbf4 7537.
package return_nil_ness

import (
	"fmt"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// Analyzer hands the package's readers the nilness of results, which they ask of one function at a time. It requires
// ctrlflow, as the build of every package it judges does, so ctrlflow has run on the package and on every package
// below it, and the resolver holds each of them, before a reader asks.
var Analyzer = &analysis.Analyzer{
	Name:       "return_nil_ness",
	Doc:        "annotates return values with their nilness when a reader asks, staticcheck's nilness on the engine's own representation",
	Run:        run,
	Requires:   []*analysis.Analyzer{ctrlflow.Analyzer},
	ResultType: reflect.TypeFor[*Result](),
}

// run is the package's nilness of results, asked of the run's resolver as the package's readers ask.
func run(pass *analysis.Pass) (any, error) {
	resolver, resolverError := fact_resolver.Of(pass)
	if resolverError != nil {
		return nil, resolverError
	}
	reader := resolver.Package(pass.Pkg)
	if reader == nil {
		return nil, fmt.Errorf("reading the nilness of %s: the fact resolver never learned it, though ctrlflow, "+
			"which it learns a package from, ran on it first", pass.Pkg.Path())
	}
	return &Result{resolver: resolver, reader: reader}, nil
}
