package function_purity

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Kind keys purity's verdicts in each package the resolver holds, and its counts.
var Kind = &fact_resolver.Kind{Name: "function_purity"}

func init() {
	fact_resolver.Register(Kind, whole,
		func(resolver *fact_resolver.Resolver, home *fact_resolver.Package, object types.Object) bool {
			function, isFunction := object.(*types.Func)
			return isFunction && judgeIn(resolver, home, function)
		}, bodilessPurity)
}

// bodilessPurity is whether a function declared without a body is pure, from its types alone (fact_resolver.Ask):
// never. Built from its declaration, it has no blocks and its syntax, so judge reads it as a stub, which
// intermediate_representation.IsStub finds no instruction in, before it reads pureStandardLibrary: even a listed one
// is not pure.
func bodilessPurity(types.Object) bool {
	return false
}

// Result is one package's purity, as its readers ask it.
type Result struct {
	resolver *fact_resolver.Resolver
	reader   *fact_resolver.Package

	// asking is the row under `--timing` whose questions these are, empty until Asking names it.
	asking string
}

// Asking is the package's purity as the rule or analyzer whose `--timing` row is named asking asks it, so the
// resolver's work for it is billed to the resolver's rows rather than to that one (fact_resolver.Question).
func (result *Result) Asking(asking string) *Result {
	return &Result{resolver: result.resolver, reader: result.reader, asking: asking}
}

// IsPure is whether function is pure as the push held it for this package: the package's own function judged pure,
// or another package's whose fact the checker would have carried here (fact_resolver.Visible) and that is pure there.
func (result *Result) IsPure(function *types.Func) bool {
	var pure bool
	result.resolver.Question(result.asking, func() { pure = ask(result.resolver, result.reader, function) })
	return pure
}

// ask is whether function is pure as a reader in reader holds it, its package's answer through fact_resolver.Ask: the
// store's, or judged in its own package, under that package's lock, once, and each answer after is the verdict kept.
func ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) bool {
	if !fact_resolver.Visible(reader, function) {
		return false
	}
	return fact_resolver.Ask(resolver, reader, Kind, function, func(home *fact_resolver.Package) bool {
		return judgeIn(resolver, home, function)
	}, func() bool { return bodilessPurity(function) })
}

// judgeIn is whether function, of home, is pure, judged there under home's lock, or the verdict kept.
func judgeIn(resolver *fact_resolver.Resolver, home *fact_resolver.Package, function *types.Func) bool {
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
		// A question answered by the push's walk of the package is a replay; a whole package for the store is not.
		resolver.Counts(Kind).Fallbacks.Add(1)
		state.fallBack(resolver, home)
		return state.pure[function]
	}
	return pure
}
