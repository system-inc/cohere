package test_failure_in_goroutine

import (
	"fmt"
	"reflect"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Kind keys the stopping calls kept in each package the resolver holds, and its counts.
//
// It is not registered with fact_resolver.Register, so no package is computed whole in it, the standard library's,
// whose entries the system cache keeps, among them. It is asked only through fact_resolver.Ask, by the rule, about a
// function of the package being analyzed. In a run with a store, the answer about a function that has an objectpath
// is recorded, can be kept in the package's entry under the package's own key, and can be given back by a store on a
// later run, which then reads no function for it. A StoppingCall names a file by its path and a call by its offsets
// there, and what keeps an answer given back right is that key, which holds the path and the bytes of each of the
// package's files (cache/keyer.go, writePortable), not the kind being unregistered.
var Kind = &fact_resolver.Kind{Name: "test_stopping_calls", OrderBound: orderFree}

// orderFree is the stopping calls' order bound (fact_resolver.Kind.OrderBound): none, and known. A function's stopping
// calls are read from its own instructions alone, following no call and reading no other answer (see callsIn), so no
// answer about any function of home hangs on the order its questions came in.
func orderFree(*fact_resolver.Package, []*types.Func) ([]*types.Func, bool) {
	return nil, true
}

// bodilessCalls is the stopping calls of a function declared without a body, from its types alone (fact_resolver.Ask):
// none. Built from its declaration, it has no blocks, so callsIn reads no call in it.
func bodilessCalls() []StoppingCall {
	return nil
}

// StoppingCallsAnalyzer hands the package's rule the stopping calls of its functions, which the rule asks of one
// function at a time. It requires ctrlflow, as the build of the package it reads does, so ctrlflow has run on the
// package and the resolver holds it before the rule asks.
var StoppingCallsAnalyzer = &analysis.Analyzer{
	Name:       "test_stopping_calls",
	Doc:        "reads which calls of a function stop the test when SA2002 asks, through the run's fact resolver",
	Run:        runStoppingCalls,
	Requires:   []*analysis.Analyzer{ctrlflow.Analyzer},
	ResultType: reflect.TypeFor[*Result](),
}

// runStoppingCalls is the package's stopping calls, asked of the run's resolver as the package's rule asks.
func runStoppingCalls(pass *analysis.Pass) (any, error) {
	resolver, resolverError := fact_resolver.Of(pass)
	if resolverError != nil {
		return nil, resolverError
	}
	reader := resolver.Package(pass.Pkg)
	if reader == nil {
		return nil, fmt.Errorf("reading the stopping calls of %s: the fact resolver never learned it, though "+
			"ctrlflow, which it learns a package from, ran on it first", pass.Pkg.Path())
	}
	return &Result{resolver: resolver, reader: reader}, nil
}

// Result is one package's stopping calls, as the rule judging it asks them.
type Result struct {
	resolver *fact_resolver.Resolver
	reader   *fact_resolver.Package

	// asking is the row under `--timing` whose questions these are, empty until Asking names it.
	asking string

	// asker is the body whose rule asks, which the resolver records beside each answer, nil until For names it.
	asker *fact_resolver.Asker
}

// Asking is the package's stopping calls as the rule whose `--timing` row is named asking asks them, so the resolver's
// work for it is billed to the resolver's rows rather than to that one (fact_resolver.Question).
func (result *Result) Asking(asking string) *Result {
	return &Result{resolver: result.resolver, reader: result.reader, asking: asking}
}

// For is the package's stopping calls as its rule asks while it checks function, so the resolver records function's
// body, or the declaration a literal of it is in, as the asker of each answer (fact_resolver.Resolver.RecordAskers,
// #5vbs1np). Where the resolver records no asker, the result itself is handed back, and nothing is made.
func (result *Result) For(function *intermediate_representation.Function) *Result {
	if !result.resolver.RecordsAskers() {
		return result
	}
	asker := fact_resolver.AskerOf(function)
	return &Result{resolver: result.resolver, reader: result.reader, asking: result.asking, asker: &asker}
}

// StoppingCalls is each call among the own instructions of function, a function or method the package declares, to a
// method of testing's common type that stops the test, in instruction order, none for a function declared without a
// body. Calls in functions that body calls in turn are not followed.
func (result *Result) StoppingCalls(function *types.Func) []StoppingCall {
	var calls []StoppingCall
	result.resolver.NoteAsker(Kind, function, result.asker)
	result.resolver.Question(result.asking, func() { calls = ask(result.resolver, result.reader, function) })
	return calls
}

// ask is the stopping calls of function, which reader declares, its answer through fact_resolver.Ask: the store's, or
// read in the package, under its lock, once, and each answer after is the one kept.
func ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) []StoppingCall {
	if function.Pkg() != reader.Types {
		// Invariant: Check asks only of a function the package it judges declares. The body of another package's is
		// never read, as the original builds none, so a question about one is a mistake in the rule, and the
		// dispatcher's recover makes it the rule's crash on the package.
		panic(fmt.Sprintf("test_failure_in_goroutine: asked the stopping calls of %s in %s, which does not declare it",
			function.FullName(), reader.Types.Path()))
	}
	return fact_resolver.Ask(resolver, reader, Kind, function, func(home *fact_resolver.Package) []StoppingCall {
		return callsIn(resolver, home, function)
	}, bodilessCalls)
}

// callsIn is the stopping calls of function, of home, read there under home's lock, or those kept. Reading builds the
// function through the resolver, so nothing need be built before the question.
func callsIn(resolver *fact_resolver.Resolver, home *fact_resolver.Package, function *types.Func) []StoppingCall {
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[keptCalls](home, Kind)
	if calls, isKept := state.calls[function]; isKept {
		return calls
	}
	declared := home.Function(function)
	if declared == nil {
		// Invariant: ask refuses a function home does not declare, and fact_resolver.Ask never computes for an
		// instance's object, so function is one the package declares, which the resolver holds a function for, made
		// from the declaration. An answer of no calls here would be a finding lost without a word, so a function the
		// resolver cannot find is a mistake in the rule or the resolver, and the rule's crash on the package.
		panic(fmt.Sprintf("test_failure_in_goroutine: the fact resolver holds no function for %s, which %s declares",
			function.FullName(), home.Types.Path()))
	}
	return state.read(resolver, function, declared)
}
