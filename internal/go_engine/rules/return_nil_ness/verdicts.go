package return_nil_ness

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// verdicts is nilness's state in one package: the nilness of each declared function's results judged, by its object,
// the defaults of its signature for one the push would hold no fact of; the functions of the package each one judged
// called, which the push would have judged with it; and the push's walk, replayed as far as an answer needed it.
type verdicts struct {
	results map[*types.Func][]ValueNilness
	callees map[*types.Func][]*intermediate_representation.Function
	replay  *replayWalk
}

// stateIn is nilness's state in home, its maps made. The caller holds home's lock.
func stateIn(home *fact_resolver.Package) *verdicts {
	state := fact_resolver.StateOf[verdicts](home, Kind)
	if state.results == nil {
		state.results = map[*types.Func][]ValueNilness{}
		state.callees = map[*types.Func][]*intermediate_representation.Function{}
	}
	return state
}

// replayOf is the push's walk of home, replayed as far as answers have needed it, begun on the first call. The caller
// holds home's lock.
func (state *verdicts) replayOf(resolver *fact_resolver.Resolver, home *fact_resolver.Package) *replayWalk {
	if state.replay == nil {
		state.replay = &replayWalk{resolver: resolver, home: home, irPackage: home.IntermediateRepresentation(),
			state: state, functions: home.SourceFunctions(), seen: map[*intermediate_representation.Function]bool{},
			begun: map[*intermediate_representation.Function]bool{}}
	}
	return state.replay
}

// whole is the nilness of every function of home whose results say more than the defaults, the push's walk replayed to
// its end, for the standard library's table, where a function absent holds no fact.
func whole(resolver *fact_resolver.Resolver, home *fact_resolver.Package) map[types.Object][]ValueNilness {
	home.Lock()
	defer home.Unlock()
	state := stateIn(home)
	resolver.Counts(Kind).Fallbacks.Add(1)
	state.replayOf(resolver, home).rest()
	answers := map[types.Object][]ValueNilness{}
	for function, results := range state.results {
		if interesting(function.Signature(), results) {
			answers[function] = results
		}
	}
	return answers
}
