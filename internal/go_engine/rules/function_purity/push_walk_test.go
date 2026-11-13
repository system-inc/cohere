package function_purity

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// facts is where the push walk reads whether a function is pure and records one it judged pure: the pass's facts, as
// the original reads and exports them.
type facts interface {
	holdsPure(function *types.Func) bool
	recordPure(function *types.Func)
}

// pushWalk is one package judged the original's way: each source function in buildir's order, with one set of every
// function whose judging has begun, so a call back into one still being judged answers not pure. It is the tests'
// record of what staticcheck answers, which the port departs from in one place only (demandWalk.isPure).
type pushWalk struct {
	facts     facts
	irPackage *intermediate_representation.Package
	seen      map[*intermediate_representation.Function]bool
}

// isPure is whether function is pure: held so by a fact, or of this package and judged so here, once, which records
// the fact. A function with no object, an anonymous one, is never judged, and one of another package that its facts
// do not hold pure is not pure.
func (walk *pushWalk) isPure(function *intermediate_representation.Function) bool {
	object := function.Object()
	if object == nil {
		return false
	}
	declared := declaredFunction(function)
	if walk.facts.holdsPure(declared) {
		return true
	}
	if function.Package != walk.irPackage {
		return false
	}
	if walk.seen[function] {
		return false
	}
	walk.seen[function] = true
	pure := judge(walk, function, declared)
	if pure {
		walk.facts.recordPure(declared)
	}
	return pure
}
