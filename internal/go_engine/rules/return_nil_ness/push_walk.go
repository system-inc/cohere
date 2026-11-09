package return_nil_ness

import (
	"fmt"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// facts is where the push walk reads the nilness a function's fact holds and records the nilness of one it judged
// worth a fact: the pass's ReturnedNilness facts, as the original reads and exports them, or the resolver's verdicts,
// when it computes a package the push's way.
type facts interface {
	imported(function *types.Func) ([]ValueNilness, bool)
	record(function *types.Func, results []ValueNilness)
}

// pushWalk is one package judged the original's way: each source function in buildir's order, with one set of every
// function whose judging has begun, shared by them all, so a call back into one already begun answers the defaults of
// its signature.
type pushWalk struct {
	facts     facts
	irPackage *intermediate_representation.Package
	seen      map[*intermediate_representation.Function]bool
}

// returns is the nilness of each of function's results: nil for a function with none, what a fact holds, or, for a
// function of this package judged here once, what judge finds, recorded as a fact when it says more than the
// defaults, and otherwise the defaults of its signature. A function with no object, an anonymous one, is never judged,
// nor one of another package, one with no blocks, one already begun, or one with no result that may be nil.
func (walk *pushWalk) returns(function *intermediate_representation.Function) []ValueNilness {
	if function.Signature.Results().Len() == 0 {
		return nil
	}
	if function.Object() == nil {
		// Closures are not judged, as staticcheck leaves them for later.
		return defaultNilnessForSignature(function.Signature)
	}
	declared := declaredFunction(function)
	if results, holds := walk.facts.imported(declared); holds {
		return results
	}
	if function.Package != walk.irPackage || function.Blocks == nil {
		return defaultNilnessForSignature(function.Signature)
	}
	if walk.seen[function] {
		// A function already begun breaks the recursion.
		return defaultNilnessForSignature(function.Signature)
	}
	walk.seen[function] = true
	if !anyPointers(function.Signature) {
		return defaultNilnessForSignature(function.Signature)
	}
	returned := (&judgment{callees: walk}).judge(function)
	if interesting(function.Signature, returned) {
		walk.facts.record(declared, returned)
	}
	return returned
}

// anyPointers is whether any result of signature may be nil.
func anyPointers(signature *types.Signature) bool {
	for result := range signature.Results().Variables() {
		if typeutil.MaybePointerLike(result.Type()) {
			return true
		}
	}
	return false
}

// declaredFunction is function's object, which has one.
func declaredFunction(function *intermediate_representation.Function) *types.Func {
	declared, isDeclared := function.Object().(*types.Func)
	if !isDeclared {
		// Invariant: a function's object is the *types.Func it was built from, so any other is a mistake in the
		// representation.
		panic(fmt.Sprintf("return_nil_ness: %s's object is a %T, not a function", function, function.Object()))
	}
	return declared
}
