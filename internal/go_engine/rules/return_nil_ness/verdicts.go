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

// keepAnswer hands the resolver results, the verdict a walk of home just kept of function, as the answer
// fact_resolver.Ask would give a reader who asked of function: what heldResults answers, the results when they say
// more than the defaults, and nil, no fact, when they do not. A walk judges a callee of home inside its caller's
// judgment and never asks the resolver of it, so without this only the function asked of had an answer of its own
// among the ones the resolver holds, and the callees whose verdicts its answer was built from had none (#kemy8sq).
// The resolver records nothing for the store of it: a callee is recorded when a reader asks of it, as before. The
// caller holds home's lock.
//
// Both walks hand over the same functions: each they judge, and each with a result that may be nil and no body, whose
// defaults they keep as its verdict, since a function of assembly in one build may be one of Go in another, with
// another answer, which a reader comparing the builds' answers needs. Neither hands over a function none of whose
// results may be nil: its signature alone answers it, the same in every build with the same types.
//
// A verdict handed over is final, the one judgeIn answers of function from then on, which the resolver needs of it,
// since it holds the answer as function's for the rest of the run (fact_resolver.Keep). Neither walk judges a function
// twice: each returns a verdict held before it would judge, so one kept is never revised, and judgeIn reads the
// verdicts held before it walks at all. And a verdict is kept only once it is the push's. The demand walk keeps one
// only while it has met no call whose answer hangs on the push's order (demandWalk.orderDependent): every function it
// judged below the one kept was judged whole, none read while begun but by itself, and whatever else it read was a
// verdict held already, final by this same reasoning, another package's answer, which Keep keeps nothing built from
// when it was a placeholder, or the defaults of a function never judged. Once it meets such a call it keeps nothing
// more, and the replay answers. The replay keeps the push's one judgment of each function, in the push's order: a
// member of a cycle that read its begun partner as the defaults is final at that, since the push never judges it
// again.
func keepAnswer(
	resolver *fact_resolver.Resolver,
	home *fact_resolver.Package,
	function *types.Func,
	results []ValueNilness,
) {
	var answer []ValueNilness
	if interesting(function.Signature(), results) {
		answer = results
	}
	fact_resolver.Keep(resolver, home, Kind, function, answer)
}

// replayOf is the push's walk of home, replayed as far as answers have needed it, begun on the first call. The caller
// holds home's lock.
//
// Its roots are home's declared functions unbuilt, in the order SourceFunctions lists them, which is the order of the
// package's Functions: a package created from its syntax holds every declared function there from its creation, and
// building them adds none. The walk builds each root as it reaches it, so a root past the last one an answer needed
// is never built, where building them all first cost a replay in a dependency package every function of the package
// (#efs50c3: 8,904 functions over a cold run at cohere's root, 0.53 of the replays' 0.60 CPU-s).
func (state *verdicts) replayOf(resolver *fact_resolver.Resolver, home *fact_resolver.Package) *replayWalk {
	if state.replay == nil {
		irPackage := home.IntermediateRepresentation()
		state.replay = &replayWalk{resolver: resolver, home: home, irPackage: irPackage, state: state,
			functions: irPackage.Functions, seen: map[*intermediate_representation.Function]bool{},
			begun: map[*intermediate_representation.Function]bool{}}
	}
	return state.replay
}

// replayUntil replays the push's walk of home until target has been judged (see replayWalk.until). A question inside
// it that ends in a panic, as one that must reload a package does (fact_resolver.Ask), leaves the walk mid-root, its
// seen set holding functions it never finished judging, which would answer later questions wrongly. So the walk is let
// go, and the next replay starts again from the first root: the verdicts it had finished are the push's, which a new
// walk holds and marks seen as it does any verdict held. The caller holds home's lock.
func (state *verdicts) replayUntil(
	resolver *fact_resolver.Resolver,
	home *fact_resolver.Package,
	target *intermediate_representation.Function,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			state.replay = nil
			panic(recovered)
		}
	}()
	state.replayOf(resolver, home).until(target)
}

// whole is the nilness of every function of home whose results say more than the defaults, the push's walk replayed to
// its end, for the standard library's table, where a function absent holds no fact.
func whole(resolver *fact_resolver.Resolver, home *fact_resolver.Package) map[types.Object][]ValueNilness {
	home.Lock()
	defer home.Unlock()
	state := stateIn(home)
	state.replayOf(resolver, home).rest()
	answers := map[types.Object][]ValueNilness{}
	for function, results := range state.results {
		if interesting(function.Signature(), results) {
			answers[function] = results
		}
	}
	return answers
}
