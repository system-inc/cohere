package return_nil_ness

import (
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// demandWalk judges one function of home and what it calls there, depth first, under home's lock.
//
// The push walks a package's functions in one order with one seen set, and a call into a function of the same package
// already begun answers the defaults of its signature, though that function may end with more. Which function is
// still begun when a call reaches it depends on where the push entered the cycle, so here the answer depends on the
// push's order, and the walk stops and ask computes the whole package the push's way. A call of a function to itself
// is not such a call: the function is begun whenever its own body is judged, in any order. Every other call answers
// as the push does in every order, since the functions a walk reaches without meeting a function begun hold no cycle.
//
// One more call depends on the order: a call through a function of no package that stands for one of home's, an
// instantiation of a generic function or a method wrapper, whose object is the declared function's. The push reads that
// one's fact, which it holds only once it has judged the declared function, so when the declared function says more
// than the defaults the walk stops the same way.
type demandWalk struct {
	resolver  *fact_resolver.Resolver
	home      *fact_resolver.Package
	irPackage *intermediate_representation.Package
	state     *verdicts
	stack     []*intermediate_representation.Function

	// orderDependent is whether the walk met a call whose answer depends on the push's order, after which it answers
	// the defaults to everything, to end at once.
	orderDependent bool
}

// returns is the nilness a call from home gets of function's results, as the push answers it (see demandWalk).
func (walk *demandWalk) returns(function *intermediate_representation.Function) []ValueNilness {
	if function.Signature.Results().Len() == 0 {
		return nil
	}
	if walk.orderDependent || function.Object() == nil {
		return defaultNilnessForSignature(function.Signature)
	}
	if function.Package == walk.irPackage {
		return walk.verdict(function)
	}
	declared := declaredFunction(function)
	if declared.Pkg() != walk.home.Types {
		if results, holds := ask(walk.resolver, walk.home, declared); holds {
			return results
		}
		return defaultNilnessForSignature(function.Signature)
	}
	standing := walk.home.Function(declared)
	if standing == nil {
		return defaultNilnessForSignature(function.Signature)
	}
	if walk.begun(standing) {
		walk.orderDependent = walk.orderDependent || standing != walk.stack[len(walk.stack)-1]
		return defaultNilnessForSignature(function.Signature)
	}
	if results := walk.verdict(standing); interesting(standing.Signature, results) {
		walk.orderDependent = true
	}
	return defaultNilnessForSignature(function.Signature)
}

// verdict is the nilness of the results of function, a declared function of home: its verdict kept, the defaults of
// its signature when it has no blocks or no result that may be nil, the defaults while it is judging itself, or judged
// now and kept.
func (walk *demandWalk) verdict(function *intermediate_representation.Function) []ValueNilness {
	declared := declaredFunction(function)
	if results, judged := walk.state.results[declared]; judged {
		return results
	}
	function.Build()
	if walk.begun(function) {
		walk.orderDependent = walk.orderDependent || function != walk.stack[len(walk.stack)-1]
		return defaultNilnessForSignature(function.Signature)
	}
	if function.Blocks == nil || !anyPointers(function.Signature) {
		results := defaultNilnessForSignature(function.Signature)
		walk.state.results[declared] = results
		return results
	}
	walk.stack = append(walk.stack, function)
	returned := (&judgment{callees: walk}).judge(function)
	walk.stack = walk.stack[:len(walk.stack)-1]
	if walk.orderDependent {
		// Judged after the walk stopped, so not judged at all; the fallback judges it.
		return defaultNilnessForSignature(function.Signature)
	}
	walk.state.results[declared] = returned
	walk.resolver.Counts(Kind).Computed.Add(1)
	return returned
}

// begun is whether function's judging has begun on this walk and not ended.
func (walk *demandWalk) begun(function *intermediate_representation.Function) bool {
	for _, judging := range walk.stack {
		if judging == function {
			return true
		}
	}
	return false
}
