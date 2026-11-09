package function_purity

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// verdicts is purity's state in one package: each declared function judged, pure or not, by its object, and whether
// the package was computed whole, the push's way.
type verdicts struct {
	pure     map[*types.Func]bool
	fellBack bool
}

// fallBack computes every function of home the push's way, in buildir's order with one seen set, and keeps each
// verdict, for a package where an answer depended on that order (see demandWalk). A verdict kept before is the push's
// by the reasoning there; one the push contradicts is counted, so the differential can require none.
func (state *verdicts) fallBack(resolver *fact_resolver.Resolver, home *fact_resolver.Package) {
	counts := resolver.Counts(Kind)
	recorded := &resolverFacts{resolver: resolver, home: home, pure: map[*types.Func]bool{}}
	walk := &pushWalk{facts: recorded, irPackage: home.IntermediateRepresentation(),
		seen: map[*intermediate_representation.Function]bool{}}
	functions := home.SourceFunctions()
	for _, function := range functions {
		walk.isPure(function)
	}
	for _, function := range functions {
		if function.Object() == nil {
			continue
		}
		declared := declaredFunction(function)
		pure := recorded.pure[declared]
		if kept, judged := state.pure[declared]; judged {
			if kept != pure {
				counts.Contradicted.Add(1)
			}
		} else {
			counts.Computed.Add(1)
		}
		state.pure[declared] = pure
	}
	state.fellBack = true
}

// whole is every function of home pure, computed whole the push's way, each with its answer, true, for the standard
// library's table, where a function absent is not pure.
func whole(resolver *fact_resolver.Resolver, home *fact_resolver.Package) map[types.Object]bool {
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[verdicts](home, Kind)
	if state.pure == nil {
		state.pure = map[*types.Func]bool{}
	}
	if !state.fellBack {
		state.fallBack(resolver, home)
	}
	answers := map[types.Object]bool{}
	for function, pure := range state.pure {
		if pure {
			answers[function] = true
		}
	}
	return answers
}
