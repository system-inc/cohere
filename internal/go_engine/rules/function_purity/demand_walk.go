package function_purity

import (
	"fmt"

	"github.com/system-inc/go-types/types"

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
//
// Each verdict judged is handed to the resolver too (fact_resolver.Keep), as the answer fact_resolver.Ask would give a
// reader who asked of function, which is the verdict itself: judgeIn answers the verdict kept. A walk judges a callee
// of home inside its caller's judgment and never asks the resolver of it, so without this only the function asked of
// had an answer of its own among the ones the resolver holds, and the callees its answer was built from had none
// (#z9fze0t). The resolver records nothing for the store of it: a callee is recorded when a reader asks of it, as
// before.
//
// A verdict handed over is final, which the resolver needs of it, since it holds the answer as function's for the
// rest of the run. It is the definition's, whatever order found it (see demandWalk), and no walk judges a function
// twice: each returns a verdict kept before it would judge. One kept while a caller further up the stack is still
// being judged is final too, since it is kept only after its own judgment returned: a callee that read a function
// still being judged as not pure is on a cycle of calls through it, and never pure in any order.
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
	fact_resolver.Keep(walk.resolver, walk.home, Kind, declared, pure)
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
