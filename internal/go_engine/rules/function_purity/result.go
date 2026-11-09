package function_purity

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Kind keys purity's verdicts in each package the resolver holds, and its counts.
var Kind = &fact_resolver.Kind{Name: "function_purity"}

// Result is one package's purity, as its readers ask it.
type Result struct {
	resolver *fact_resolver.Resolver
	reader   *fact_resolver.Package
}

// IsPure is whether function is pure as the push held it for this package: the package's own function judged pure,
// or another package's whose fact the checker would have carried here (fact_resolver.Visible) and that is pure there.
func (result *Result) IsPure(function *types.Func) bool {
	return ask(result.resolver, result.reader, function)
}

// ask is whether function is pure as a reader in reader holds it. The function is judged in its own package, under
// that package's lock, once, and each answer after is the verdict kept.
func ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) bool {
	if !fact_resolver.Visible(reader, function) {
		return false
	}
	home := resolver.Package(function.Pkg())
	if home == nil {
		return false
	}
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[verdicts](home, Kind)
	if state.pure == nil {
		state.pure = map[*types.Func]bool{}
	}
	if pure, judged := state.pure[function]; judged {
		return pure
	}
	declared := home.Function(function)
	if declared == nil {
		// No declared function of the package, as an instantiated method's object is not, which the push never
		// judged and so never held pure.
		return false
	}
	walk := &demandWalk{resolver: resolver, home: home, irPackage: home.IntermediateRepresentation(), state: state,
		stack: map[*intermediate_representation.Function]bool{}}
	pure := walk.verdict(declared)
	if walk.orderDependent {
		state.fallBack(resolver, home)
		return state.pure[function]
	}
	return pure
}
