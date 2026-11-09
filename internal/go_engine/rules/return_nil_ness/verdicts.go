package return_nil_ness

import (
	"go/types"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// verdicts is nilness's state in one package: the nilness of each declared function's results judged, by its object,
// the defaults of its signature for one the push would hold no fact of, and whether the package was computed whole,
// the push's way.
type verdicts struct {
	results  map[*types.Func][]ValueNilness
	fellBack bool
}

// fallBack computes every function of home the push's way, in buildir's order with one seen set, and keeps each
// verdict, for a package where an answer depended on that order (see demandWalk). A verdict kept before is the push's
// by the reasoning there; one the push contradicts is counted, so the differential can require none.
func (state *verdicts) fallBack(resolver *fact_resolver.Resolver, home *fact_resolver.Package) {
	counts := resolver.Counts(Kind)
	counts.Fallbacks.Add(1)
	recorded := &resolverFacts{resolver: resolver, home: home, results: map[*types.Func][]ValueNilness{}}
	walk := &pushWalk{facts: recorded, irPackage: home.IntermediateRepresentation(),
		seen: map[*intermediate_representation.Function]bool{}}
	functions := home.SourceFunctions()
	for _, function := range functions {
		walk.returns(function)
	}
	for _, function := range functions {
		if function.Object() == nil {
			continue
		}
		declared := declaredFunction(function)
		results, holds := recorded.results[declared]
		if !holds {
			results = defaultNilnessForSignature(function.Signature)
		}
		if kept, judged := state.results[declared]; judged {
			if !slices.Equal(kept, results) {
				counts.Contradicted.Add(1)
			}
		} else {
			counts.Computed.Add(1)
		}
		state.results[declared] = results
	}
	state.fellBack = true
}
