//cohere:file-ignore consistency-require-separated-folder-words staticcheck's nilness, a project's own spelling

// Package return_nilness judges whether each function's results may be nil, staticcheck's nilness facts ported onto the
// engine's own intermediate representation, so the readers asking what a function returns read them without buildir
// (#w7zmvat).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 analysis/facts/nilness/nilness.go, whose algorithm it
// keeps step for step: the package's source functions judged in the order buildir lists them, with one set of the
// functions whose judging has begun shared by them all, a callee of the same package judged as it is met, a callee
// already begun, as in a cycle of calls, answering the defaults of its signature, and each function's results judged
// by a forward data flow analysis over its blocks, forward_flow's port of staticcheck's dense.Forward, on staticcheck's
// own dfa lattices. A function whose results say more than that each may be nil is marked with a ReturnedNilness fact
// on its object, which the packages importing it read. Its Result answers the nilness of any function's result from
// the facts, this package's and its imports', as staticcheck's does.
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
package return_nilness

import (
	"fmt"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// Analyzer judges the results of each function of the package, exporting ReturnedNilness on those whose results say
// more than that each may be nil, and returns every function's results the facts hold, the imported packages' among
// them.
var Analyzer = &analysis.Analyzer{
	Name:       "return_nilness",
	Doc:        "annotates return values with their nilness, staticcheck's nilness facts on the engine's own representation",
	Run:        run,
	Requires:   []*analysis.Analyzer{build_intermediate_representation.Analyzer},
	FactTypes:  []analysis.Fact{new(ReturnedNilness)},
	ResultType: reflect.TypeFor[*Result](),
}

// run judges every source function of the package, in buildir's order, and returns every function's results the facts
// hold.
func run(pass *analysis.Pass) (any, error) {
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	judging := &judgment{
		pass:      pass,
		irPackage: built.Package(),
		seen:      map[*intermediate_representation.Function]bool{},
	}
	for function := range built.SourceFunctions() {
		judging.returns(function)
	}

	result := &Result{functions: map[*types.Func][]ValueNilness{}}
	for _, fact := range pass.AllObjectFacts() {
		function, isFunction := fact.Object.(*types.Func)
		returned, isReturned := fact.Fact.(*ReturnedNilness)
		if !isFunction || !isReturned {
			return nil, fmt.Errorf("reading return_nilness's facts: %s holds a %T, where only a function's "+
				"ReturnedNilness was expected", fact.Object, fact.Fact)
		}
		result.functions[function] = returned.Results
	}
	return result, nil
}
