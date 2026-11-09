package return_nil_ness

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// resolverFacts is the push walk's facts in a fallback: home's own functions as the walk records them, and every
// other package's asked of the resolver, as a reader in home would.
type resolverFacts struct {
	resolver *fact_resolver.Resolver
	home     *fact_resolver.Package
	results  map[*types.Func][]ValueNilness
}

func (recorded *resolverFacts) imported(function *types.Func) ([]ValueNilness, bool) {
	if function.Pkg() == recorded.home.Types {
		results, holds := recorded.results[function]
		return results, holds
	}
	return ask(recorded.resolver, recorded.home, function)
}

func (recorded *resolverFacts) record(function *types.Func, results []ValueNilness) {
	recorded.results[function] = results
}
