package return_nil_ness

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// Kind keys nilness's verdicts in each package the resolver holds, and its counts.
var Kind = &fact_resolver.Kind{Name: "return_nil_ness"}

func init() {
	fact_resolver.Register(Kind, whole)
}

// Result is one package's nilness of results, as its readers ask it.
type Result struct {
	resolver *fact_resolver.Resolver
	reader   *fact_resolver.Package
}

// Nilness is the nilness of the result at index result of function: {_ NeverNil} for a result that cannot be nil,
// {MaybeNil MaybeNil} for a function the push held no fact of here, and otherwise what the fact holds, normalized, as
// staticcheck's nilness.Result answers it.
func (judged *Result) Nilness(function *types.Func, result int) ValueNilness {
	resultType := function.Signature().Results().At(result).Type()
	if !typeutil.MaybePointerLike(resultType) {
		return ValueNilness{Outer: NeverNil}
	}
	results, holds := ask(judged.resolver, judged.reader, function)
	if !holds {
		return ValueNilness{Inner: MaybeNil, Outer: MaybeNil}
	}
	return normalize(results[result], resultType)
}

// ask is the nilness of function's results and whether a reader in reader holds them as the push did, as a fact: the
// fact crossed here (fact_resolver.Visible) and says more than the defaults. Its package's answer comes through
// fact_resolver.Ask, the results of a fact, nil for none: the store's, or judged in its own package, under that
// package's lock, once, and each answer after is the verdict kept.
func ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) ([]ValueNilness, bool) {
	if !fact_resolver.Visible(reader, function) {
		return nil, false
	}
	results := fact_resolver.Ask(resolver, Kind, function, func(home *fact_resolver.Package) []ValueNilness {
		judged, holds := judgeIn(resolver, home, function)
		if !holds {
			return nil
		}
		return judged
	})
	return results, results != nil
}

// judgeIn is the nilness of function's results, of home, and whether they say more than the defaults, judged there
// under home's lock, or the verdict kept.
func judgeIn(
	resolver *fact_resolver.Resolver,
	home *fact_resolver.Package,
	function *types.Func,
) ([]ValueNilness, bool) {
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[verdicts](home, Kind)
	if state.results == nil {
		state.results = map[*types.Func][]ValueNilness{}
	}
	if results, judged := state.results[function]; judged {
		return results, interesting(function.Signature(), results)
	}
	declared := home.Function(function)
	if declared == nil {
		// No declared function of the package, as an instantiated method's object is not, which the push never
		// judged and so never held a fact of.
		return nil, false
	}
	walk := &demandWalk{resolver: resolver, home: home, irPackage: home.IntermediateRepresentation(), state: state}
	results := walk.verdict(declared)
	if walk.orderDependent {
		state.fallBack(resolver, home)
		results = state.results[function]
	}
	return results, interesting(function.Signature(), results)
}

// normalize fills what value leaves unknown with MaybeNil, and Inner too for a value of valueType that is no interface,
// where Inner means nothing.
func normalize(value ValueNilness, valueType types.Type) ValueNilness {
	if value.Inner == 0 || !types.IsInterface(valueType) {
		value.Inner = MaybeNil
	}
	if value.Outer == 0 {
		value.Outer = MaybeNil
	}
	return value
}
