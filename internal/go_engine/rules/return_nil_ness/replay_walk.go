package return_nil_ness

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// replayWalk is the push's walk of one package, replayed only as far as an answer needs it: its source functions in
// buildir's order, each judged with the one seen set they all share, resumed where the last replay stopped.
//
// A function's answer in the push depends on the order only through the cycles of calls it reaches: which member of a
// cycle the walk enters first decides which call back answers the defaults. Everything else is the same in any order
// (see demandWalk). The member entered first is the first one the walk reaches, so the walk is replayed root by root
// until the function asked of has been judged, and no further: every cycle that function reaches is then entered as
// the push enters it.
//
// A root the push would judge whose verdict the resolver already holds, judged on demand without meeting a cycle, is
// not judged again: the push would judge it and the functions below it, each to the verdict already held, so the
// replay marks them seen, as the push would, and moves on. Being seen is what the push's facts on a wrapper's callee
// read, so it must be what the push's walk leaves.
type replayWalk struct {
	resolver  *fact_resolver.Resolver
	home      *fact_resolver.Package
	irPackage *intermediate_representation.Package
	state     *verdicts

	functions []*intermediate_representation.Function
	next      int
	seen      map[*intermediate_representation.Function]bool
	begun     map[*intermediate_representation.Function]bool
}

// until replays the push's walk root by root until target, a declared function of home, has been judged.
func (walk *replayWalk) until(target *intermediate_representation.Function) {
	for walk.next < len(walk.functions) && !walk.judged(target) {
		root := walk.functions[walk.next]
		walk.next++
		walk.returns(root)
	}
}

// rest replays the push's walk to its end, every root judged as the push judges it.
func (walk *replayWalk) rest() {
	for walk.next < len(walk.functions) {
		root := walk.functions[walk.next]
		walk.next++
		walk.returns(root)
	}
}

// judged is whether the replay has judged function to the end, or left it at the defaults of its signature.
func (walk *replayWalk) judged(function *intermediate_representation.Function) bool {
	return walk.seen[function] && !walk.begun[function]
}

// returns is the push's answer for the nilness of function's results at this point of its walk (see pushWalk), the
// verdict held standing in for a judgment the push would make again.
func (walk *replayWalk) returns(function *intermediate_representation.Function) []ValueNilness {
	if function.Signature.Results().Len() == 0 {
		return nil
	}
	if function.Object() == nil {
		return defaultNilnessForSignature(function.Signature)
	}
	declared := declaredFunction(function)
	if results, holds := walk.imported(declared); holds {
		return results
	}
	if function.Package != walk.irPackage {
		return defaultNilnessForSignature(function.Signature)
	}
	walk.resolver.Build(function)
	if function.Blocks == nil || walk.seen[function] {
		return defaultNilnessForSignature(function.Signature)
	}
	walk.seen[function] = true
	if !anyPointers(function.Signature) {
		return defaultNilnessForSignature(function.Signature)
	}
	if results, held := walk.state.results[declared]; held {
		walk.markSeen(declared)
		return results
	}
	walk.begun[function] = true
	recorder := &calleeRecorder{callees: walk, irPackage: walk.irPackage}
	returned := (&judgment{callees: recorder}).judge(function)
	delete(walk.begun, function)
	walk.state.results[declared] = returned
	walk.state.callees[declared] = recorder.recorded
	walk.resolver.Counts(Kind).Computed.Add(1)
	return returned
}

// imported is the fact the push would hold on function here: another package's, as the resolver answers it, or one of
// home's that the walk has judged to the end and found to say more than the defaults.
func (walk *replayWalk) imported(function *types.Func) ([]ValueNilness, bool) {
	if function.Pkg() != walk.home.Types {
		return ask(walk.resolver, walk.home, function)
	}
	declared := walk.home.Function(function)
	if declared == nil || !walk.judged(declared) {
		return nil, false
	}
	results, held := walk.state.results[function]
	if !held || !interesting(declared.Signature, results) {
		return nil, false
	}
	return results, true
}

// markSeen marks seen every function of home that judging function, held already, calls, and each one's in turn, as
// the push's judgment of function would have.
func (walk *replayWalk) markSeen(function *types.Func) {
	for _, callee := range walk.state.callees[function] {
		walk.resolver.Build(callee)
		if callee.Blocks == nil || walk.seen[callee] {
			continue
		}
		walk.seen[callee] = true
		if anyPointers(callee.Signature) {
			walk.markSeen(declaredFunction(callee))
		}
	}
}
