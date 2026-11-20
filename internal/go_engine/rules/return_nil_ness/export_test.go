package return_nil_ness

import (
	"go/types"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// NewPushAnalyzer is newPushAnalyzer, for the differential beside this file, which runs it beside the reader that
// imports this package and so sits outside it.
func NewPushAnalyzer(judged *atomic.Int64) *analysis.Analyzer {
	return newPushAnalyzer(judged)
}

// PushNilness is the nilness push's result on pass's package holds of the result at index result of function, and
// whether pass holds push's result at all.
func PushNilness(pass *analysis.Pass, push *analysis.Analyzer, function *types.Func, result int) (ValueNilness, bool) {
	pushed, isPushed := pass.ResultOf[push].(*pushResult)
	if !isPushed {
		return ValueNilness{}, false
	}
	return pushed.Nilness(function, result), true
}

// Judged is heldResults, the nilness of function's results as the kind's own verdict in home holds them, nil for no
// fact: what Ask computes, read from the kind's state and never from the resolver's memo.
func Judged(resolver *fact_resolver.Resolver, home *fact_resolver.Package, function *types.Func) []ValueNilness {
	return heldResults(resolver, home, function)
}

// Ask is ask, the nilness of function's results and whether a reader in reader holds them as a fact.
func Ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) ([]ValueNilness, bool) {
	return ask(resolver, reader, function)
}

// HeldVerdicts is every verdict nilness's own state holds in home of a function with a result that may be nil, by its
// function, each as the answer fact_resolver.Ask would give of it: its results when they say more than the defaults,
// and nil, no fact, when they do not. Read under home's lock. A function none of whose results may be nil is left
// out: its verdict is the defaults of its signature, read from no body and handed to no resolver (keepAnswer).
func HeldVerdicts(home *fact_resolver.Package) map[*types.Func][]ValueNilness {
	home.Lock()
	defer home.Unlock()
	held := map[*types.Func][]ValueNilness{}
	for function, results := range stateIn(home).results {
		if !anyPointers(function.Signature()) {
			continue
		}
		held[function] = nil
		if interesting(function.Signature(), results) {
			held[function] = results
		}
	}
	return held
}
