package odd_slice_argument

import (
	"go/types"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// evenByDefinition marks a slice parameter the definition holds marked, as the definition's analyzer exports it, apart
// from the push's evenElements so the two never read each other's facts.
type evenByDefinition struct{}

// AFact makes evenByDefinition an analysis.Fact.
func (*evenByDefinition) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

func (*evenByDefinition) String() string { return "needs even elements by definition" }

// definitionResult is every parameter the definition holds marked in a package: its own, and its imports' that the
// facts carried there.
type definitionResult map[types.Object]bool

// definitionAnalyzer marks each package by the definition alone, with no walk and so no order (#8d9r2rh). A parameter
// is marked exactly when it is in the least set that holds every parameter its function checks directly
// (directChecks), and every slice parameter a function passes as it is, in a static call in a block that dominates
// every return, to a parameter in the set. It starts from the direct checks, adds every parameter some call passes to
// a parameter it already holds, and repeats until a pass over every call adds none. A mark is only ever added, and
// only because of marks already there, so that set exists and is one set whatever order finds it, a cycle of calls
// included, where each member waits on another until one of them is marked from outside the cycle.
//
// The functions it reads are the package's source functions and each function of no package, an instantiation or a
// wrapper, that they reach through such calls. A parameter of another package is marked when its fact says so, as the
// push reads one and as the resolver answers one. It is the oracle every way the rule computes its marks is held to,
// independent of them all: it shares only what they read of one body, directChecks, passedParameters and
// dominatesEveryReturn.
var definitionAnalyzer = &analysis.Analyzer{
	Name:       "odd_slice_argument_definition",
	Doc:        "marks the parameters that need an even length by the definition, a least fixed point with no order",
	Run:        runDefinition,
	Requires:   []*analysis.Analyzer{build_intermediate_representation.Analyzer},
	FactTypes:  []analysis.Fact{new(evenByDefinition)},
	ResultType: reflect.TypeFor[definitionResult](),
}

// runDefinition is the package's least fixed point, its own parameters exported as facts, and every parameter the
// definition holds marked there.
func runDefinition(pass *analysis.Pass) (any, error) {
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	functions := definitionFunctions(slices.Collect(built.SourceFunctions()))
	marked := map[types.Object]bool{}
	holds := func(parameter *types.Var) bool {
		if parameter.Pkg() == pass.Pkg {
			return marked[parameter]
		}
		return pass.ImportObjectFact(parameter, new(evenByDefinition))
	}
	var passes []passedParameter
	for _, function := range functions {
		for _, parameter := range directChecks(function) {
			marked[parameter.Object()] = true
		}
		for _, call := range definitionCalls(function) {
			passes = append(passes, passedParameters(call, call.Call.StaticCallee())...)
		}
	}
	for added := true; added; {
		added = false
		for _, passed := range passes {
			if marked[passed.parameter.Object()] || !holds(passed.calleeParameter) {
				continue
			}
			marked[passed.parameter.Object()] = true
			added = true
		}
	}
	result := definitionResult{}
	for parameter := range marked {
		// A wrapper built here for another package's function holds that function's parameters as its own. Their
		// marks are that package's to make, read through its facts, so one made here is neither exported nor kept.
		if parameter.Pkg() != pass.Pkg {
			continue
		}
		pass.ExportObjectFact(parameter, new(evenByDefinition))
		result[parameter] = true
	}
	for _, fact := range pass.AllObjectFacts() {
		if _, isMark := fact.Fact.(*evenByDefinition); isMark {
			result[fact.Object] = true
		}
	}
	return result, nil
}

// definitionCalls is each static call of function in a block that dominates every return, the calls the definition
// reads a pass from.
func definitionCalls(function *intermediate_representation.Function) []*intermediate_representation.Call {
	var calls []*intermediate_representation.Call
	for _, block := range function.Blocks {
		if !dominatesEveryReturn(function, block) {
			continue
		}
		for _, instruction := range block.Instructions {
			call, isCall := instruction.(*intermediate_representation.Call)
			if isCall && call.Call.StaticCallee() != nil {
				calls = append(calls, call)
			}
		}
	}
	return calls
}

// definitionFunctions is every function the definition reads in a package: its source functions, and each function of
// no package, which the representation makes where a call needs it and lists nowhere, that one of them reaches through
// the calls the definition reads, however many such functions lie between.
func definitionFunctions(
	sourceFunctions []*intermediate_representation.Function,
) []*intermediate_representation.Function {
	functions := slices.Clone(sourceFunctions)
	listed := map[*intermediate_representation.Function]bool{}
	for _, function := range functions {
		listed[function] = true
	}
	// The list grows as it is read, so a function of no package is read for the ones it reaches in turn.
	for index := 0; index < len(functions); index++ {
		for _, call := range definitionCalls(functions[index]) {
			callee := call.Call.StaticCallee()
			if callee.Package != nil || listed[callee] {
				continue
			}
			listed[callee] = true
			functions = append(functions, callee)
		}
	}
	return functions
}
