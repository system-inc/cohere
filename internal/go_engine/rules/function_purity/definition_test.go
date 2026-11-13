package function_purity

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// IsPureByDefinition marks a function the definition holds pure (see demandWalk), as the definition's analyzer
// exports it, apart from the push's IsPure so the two never read each other's facts.
type IsPureByDefinition struct{}

func (*IsPureByDefinition) AFact() {
	// AFact only marks the type as a fact, so it does nothing.
}

func (*IsPureByDefinition) String() string { return "is pure by definition" }

// definitionResult is every function the definition holds pure in a package, its own and its imports' that the facts
// carried there.
type definitionResult map[*types.Func]bool

// definitionAnalyzer judges each package by the definition alone, with no walk and so no order: starting from no
// function pure, it adds every function of the package whose body judge allows given what it holds pure so far, and
// repeats until a pass adds none. That is the least set closed under judge, which a cycle of calls never enters, since
// each of its functions waits on another. A function of another package is pure when its fact says so, as the push
// and the resolver read one. It is the oracle the demand walk is held to, independent of it.
var definitionAnalyzer = &analysis.Analyzer{
	Name:       "function_purity_definition",
	Doc:        "marks the functions that are pure by the definition, a least fixed point with no order",
	Run:        runDefinition,
	Requires:   []*analysis.Analyzer{build_intermediate_representation.Analyzer},
	FactTypes:  []analysis.Fact{new(IsPureByDefinition)},
	ResultType: reflect.TypeFor[definitionResult](),
}

// definitionCallees answers judge's question from the set so far, and from the facts for another package's function.
type definitionCallees struct {
	pass *analysis.Pass
	pure map[*types.Func]bool
}

func (callees definitionCallees) isPure(function *intermediate_representation.Function) bool {
	if function.Object() == nil {
		return false
	}
	declared := declaredFunction(function)
	if declared.Pkg() == callees.pass.Pkg {
		return callees.pure[declared]
	}
	return callees.pass.ImportObjectFact(declared, new(IsPureByDefinition))
}

// runDefinition is the package's least fixed point, exported as facts, and every function the facts hold pure.
func runDefinition(pass *analysis.Pass) (any, error) {
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	callees := definitionCallees{pass: pass, pure: map[*types.Func]bool{}}
	functions := slices.Collect(built.SourceFunctions())
	for added := true; added; {
		added = false
		for _, function := range functions {
			if function.Object() == nil {
				continue
			}
			declared := declaredFunction(function)
			if callees.pure[declared] || !judge(callees, function, declared) {
				continue
			}
			callees.pure[declared] = true
			added = true
		}
	}
	for function := range callees.pure {
		pass.ExportObjectFact(function, &IsPureByDefinition{})
	}
	result := definitionResult{}
	for _, fact := range pass.AllObjectFacts() {
		function, isFunction := fact.Object.(*types.Func)
		if _, isPure := fact.Fact.(*IsPureByDefinition); !isFunction || !isPure {
			return nil, fmt.Errorf("reading the definition's facts: %s holds a %T, where only a function's "+
				"IsPureByDefinition was expected", fact.Object, fact.Fact)
		}
		result[function] = true
	}
	return result, nil
}
