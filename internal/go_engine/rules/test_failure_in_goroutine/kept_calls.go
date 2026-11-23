package test_failure_in_goroutine

import (
	"github.com/system-inc/go-types/types"

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
// kept. No call is a nil answer, the zero Value fact_resolver.Ask answers for no fact. The caller holds the package's
// lock.
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
