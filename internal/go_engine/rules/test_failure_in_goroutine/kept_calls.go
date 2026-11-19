package test_failure_in_goroutine

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// keptCalls is the kind's state in one package: the stopping calls of each declared function read, by its object, nil
// for a function read that has none. A function's calls are read from its own instructions alone, so one kept by any
// question answers every later one.
type keptCalls struct {
	calls map[*types.Func][]StoppingCall
}

// read is the stopping calls among the own instructions of declared, the built function whose object is function,
// kept. No call is a nil answer, the zero Value a fact resolver's store holds no answer for. The caller holds the
// package's lock.
func (state *keptCalls) read(
	resolver *fact_resolver.Resolver,
	function *types.Func,
	declared *intermediate_representation.Function,
) []StoppingCall {
	if state.calls == nil {
		state.calls = map[*types.Func][]StoppingCall{}
	}
	var calls []StoppingCall
	for call, name := range stoppingCalls(declared) {
		calls = append(calls, newStoppingCall(call, name))
	}
	state.calls[function] = calls
	resolver.Counts(Kind).Computed.Add(1)
	return calls
}

// whole is the stopping calls of every function of home that has any, each source function read, or its calls kept,
// for the entries a run writes and the standard library's table, where a function absent has none.
func whole(resolver *fact_resolver.Resolver, home *fact_resolver.Package) map[types.Object][]StoppingCall {
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[keptCalls](home, Kind)
	answers := map[types.Object][]StoppingCall{}
	for _, declared := range home.SourceFunctions() {
		function, isFunction := declared.Object().(*types.Func)
		if !isFunction {
			// The package's synthetic initializer, which has no object and so no answer a question could name.
			continue
		}
		calls, isKept := state.calls[function]
		if !isKept {
			calls = state.read(resolver, function, declared)
		}
		if len(calls) > 0 {
			answers[function] = calls
		}
	}
	return answers
}
