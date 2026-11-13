package function_purity

import (
	"fmt"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// demandWalk judges one function of home and what it calls there, depth first, under home's lock.
//
// Purity is defined so that no order of judging changes it (#zva1zrc): a function is pure when its own body allows it
// and every function it calls, other than itself directly, is pure, and a cycle of calls is never pure. That is the
// least set closed under those rules, which exists and is one set whatever order finds it.
//
// The walk finds it from anywhere. A static call into a function still being judged answers not pure, and that is
// never wrong: every function between the callee and the call on the stack then answers not pure, the callee among
// them, and each of them is on a cycle through the callee, so none is pure under the definition. A function judged
// before is its verdict kept, and one judged now is kept too.
//
// staticcheck's push walks the package in buildir's order instead, and answers a call through a function of no package
// that stands for one of home's, an instantiation of a generic function or a method wrapper, by reading the declared
// function's fact, which it holds only once it has judged that function. Its answer there turns on whether the push
// reached the declared function first, so on what else the package declares and in which file. This walk answers that
// call as it answers a call of the declared function itself, its verdict, judged on demand: the one place the port
// departs from the original, by answering pure where the push's order alone made a call not pure.
type demandWalk struct {
	resolver  *fact_resolver.Resolver
	home      *fact_resolver.Package
	irPackage *intermediate_representation.Package
	state     *verdicts
	stack     map[*intermediate_representation.Function]bool
}

// isPure is whether a call from home to function leaves its caller pure (see demandWalk).
func (walk *demandWalk) isPure(function *intermediate_representation.Function) bool {
	if function.Object() == nil {
		return false
	}
	declared := declaredFunction(function)
	if function.Package == walk.irPackage {
		return walk.verdict(function)
	}
	if declared.Pkg() != walk.home.Types {
		return ask(walk.resolver, walk.home, declared)
	}
	// An instantiation or a method wrapper of one of home's functions answers as the declared function does.
	standing := walk.home.Function(declared)
	if standing == nil {
		return false
	}
	return walk.verdict(standing)
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
	walk.state.pure[declared] = pure
	walk.resolver.Counts(Kind).Computed.Add(1)
	return pure
}

// declaredFunction is function's object, which has one.
func declaredFunction(function *intermediate_representation.Function) *types.Func {
	declared, isDeclared := function.Object().(*types.Func)
	if !isDeclared {
		// Invariant: a function's object is the *types.Func it was built from, so any other is a mistake in the
		// representation, which the original's assertion would panic on too.
		panic(fmt.Sprintf("function_purity: %s's object is a %T, not a function", function, function.Object()))
	}
	return declared
}
