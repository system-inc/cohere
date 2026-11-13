package function_purity

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// verdicts is purity's state in one package: each declared function judged, pure or not, by its object. Every verdict
// is the definition's (see demandWalk), so one kept by any question answers every later one.
type verdicts struct {
	pure map[*types.Func]bool
}

// walk is a demand walk over home's verdicts, with state's map made when it is the package's first.
func (state *verdicts) walk(resolver *fact_resolver.Resolver, home *fact_resolver.Package) *demandWalk {
	if state.pure == nil {
		state.pure = map[*types.Func]bool{}
	}
	return &demandWalk{resolver: resolver, home: home, irPackage: home.IntermediateRepresentation(), state: state,
		stack: map[*intermediate_representation.Function]bool{}}
}

// whole is every function of home pure, each source function judged, or its verdict kept, each with its answer, true,
// for the standard library's table, where a function absent is not pure. The order the functions are judged in
// changes no verdict (see demandWalk).
func whole(resolver *fact_resolver.Resolver, home *fact_resolver.Package) map[types.Object]bool {
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[verdicts](home, Kind)
	walk := state.walk(resolver, home)
	for _, function := range home.SourceFunctions() {
		if function.Object() != nil {
			walk.verdict(function)
		}
	}
	answers := map[types.Object]bool{}
	for function, pure := range state.pure {
		if pure {
			answers[function] = true
		}
	}
	return answers
}
