package function_purity

import (
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// demandWalk judges one function of home and what it calls there, depth first, under home's lock.
//
// The push walks a package's functions in one order with one seen set, and its answer for a static call into a
// function of the same package is the callee's verdict, or not pure while the callee is still being judged. That last
// is never wrong: every function between the callee and the call on the stack then answers not pure, the callee
// among them, so a function still being judged is one whose verdict is not pure, in any order, as a cycle of calls is
// never pure. So this walk, entered anywhere, keeps the push's answer for every such call.
//
// One call is not: a call through a function of no package that stands for one of home's, an instantiation of a
// generic function or a method wrapper, whose object is the declared function's. The push reads that one's fact, which
// it holds only once it has judged the declared function, so when the declared function is pure the answer depends on
// whether the push reached it first. The walk then stops, and ask computes the whole package the push's way.
type demandWalk struct {
	resolver  *fact_resolver.Resolver
	home      *fact_resolver.Package
	irPackage *intermediate_representation.Package
	state     *verdicts
	stack     map[*intermediate_representation.Function]bool

	// orderDependent is whether the walk met a call whose answer depends on the push's order, after which it answers
	// not pure to everything, to end at once.
	orderDependent bool
}

// isPure is whether a call from home to function leaves its caller pure, as the push answers it (see demandWalk).
func (walk *demandWalk) isPure(function *intermediate_representation.Function) bool {
	if walk.orderDependent || function.Object() == nil {
		return false
	}
	declared := declaredFunction(function)
	if function.Package == walk.irPackage {
		return walk.verdict(function)
	}
	if declared.Pkg() != walk.home.Types {
		return ask(walk.resolver, walk.home, declared)
	}
	standing := walk.home.Function(declared)
	if standing == nil || walk.stack[standing] || !walk.verdict(standing) {
		return false
	}
	walk.orderDependent = true
	return false
}

// verdict is whether function, a declared function of home, is pure: its verdict kept, not pure while it is still
// being judged, or judged now and kept.
func (walk *demandWalk) verdict(function *intermediate_representation.Function) bool {
	declared := declaredFunction(function)
	if pure, judged := walk.state.pure[declared]; judged {
		return pure
	}
	if walk.stack[function] {
		return false
	}
	walk.stack[function] = true
	walk.resolver.Build(function)
	pure := judge(walk, function, declared)
	delete(walk.stack, function)
	if walk.orderDependent {
		// Judged after the walk stopped, so not judged at all; the fallback judges it.
		return false
	}
	walk.state.pure[declared] = pure
	walk.resolver.Counts(Kind).Computed.Add(1)
	return pure
}
