package function_purity

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// resolverFacts is the push walk's facts in a fallback: home's own functions as the walk records them, and every
// other package's asked of the resolver, as a reader in home would.
type resolverFacts struct {
	resolver *fact_resolver.Resolver
	home     *fact_resolver.Package
	pure     map[*types.Func]bool
}

func (recorded *resolverFacts) holdsPure(function *types.Func) bool {
	if function.Pkg() == recorded.home.Types {
		return recorded.pure[function]
	}
	return ask(recorded.resolver, recorded.home, function)
}

func (recorded *resolverFacts) recordPure(function *types.Func) {
	recorded.pure[function] = true
}
