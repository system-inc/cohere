package odd_slice_argument

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// demandWalk settles the marks of one function of home and of every function those marks rest on, under home's lock.
//
// The marks are defined so that no order of judging changes them (#8d9r2rh). A parameter is marked exactly when it is
// in the least set that holds every parameter its function checks directly (directChecks), and every slice parameter a
// function passes as it is, in a static call in a block that dominates every return, to a parameter in the set. A mark
// is only ever added, and only because of marks already there, so that set exists and is one set whatever order finds
// it.
//
// The walk finds it from anywhere. From the function asked of it reaches every function a pass leads to, a function of
// home or one of no package, an instantiation or a wrapper, which it reads as it reads any other, and stops at a
// function whose marks are final already. Nothing a reached function's marks rest on lies outside what was reached,
// the final ones and other packages' parameters, whose marks are their packages' answers. So the definition, passed
// over the reached functions' passes until a pass adds nothing, leaves every one of them final at once, and they are
// kept together. No function's marks are ever read before they are final.
//
// staticcheck's walk visits each function once, in buildir's order, a callee of the same package or of none first, and
// reads a function already begun, and one it never enters because a function of no package called it, as far as its
// marks have come. In a cycle of calls, and through an instantiation or a wrapper, its marks then turn on where it
// entered, which is the order the package declares its functions in, and around a cycle a mark must cross more than
// once it misses them in every order. Those are the places the port departs from the original, by marking what the
// original's order left out.
type demandWalk struct {
	resolver  *fact_resolver.Resolver
	home      *fact_resolver.Package
	irPackage *intermediate_representation.Package
	state     *marks

	// reached is each function reached whose marks were not final, in the order its reading ended: a callee before
	// its caller, wherever no cycle of calls holds them both. byFunction is the same functions, each from the moment
	// its reading began.
	reached    []*reachedFunction
	byFunction map[*intermediate_representation.Function]*reachedFunction

	// cyclic is whether a pass led to a function whose reading had begun and not ended, which closes a cycle of calls,
	// a function calling itself among them. Without one, every pass in reached leads to a function before it, or to a
	// final one, and a single pass over reached settles every mark.
	cyclic bool
}

// reachedFunction is one function the walk reached: its marks so far, from its own checks until settle ends, and each
// pass its body makes.
type reachedFunction struct {
	function *intermediate_representation.Function
	marked   map[types.Object]bool
	passes   []reachedPass

	// read is whether the function's body has been read to its end.
	read bool
}

// reachedPass is one pass the definition reads: a slice parameter passed as it is to a callee's parameter. callee is
// the function that holds the callee's parameter's mark, a function of home or of none, reached or final, and nil for
// another package's parameter, whose mark is asked of the resolver.
type reachedPass struct {
	passedParameter
	callee *intermediate_representation.Function
}

// begin reaches function, a function of home, unless its marks are final or the walk has reached it.
func (walk *demandWalk) begin(function *intermediate_representation.Function) {
	if _, final := walk.state.functions[function]; final {
		return
	}
	if _, isReached := walk.byFunction[function]; !isReached {
		walk.reach(function)
	}
}

// reach reads function, a function of home or of none whose marks are not final and that the walk has not reached, and
// every such function its passes lead to, each once.
func (walk *demandWalk) reach(function *intermediate_representation.Function) {
	walk.resolver.Build(function)
	reached := &reachedFunction{function: function, marked: map[types.Object]bool{}}
	walk.byFunction[function] = reached
	for _, parameter := range directChecks(function) {
		reached.marked[parameter.Object()] = true
	}
	for _, block := range function.Blocks {
		if !dominatesEveryReturn(function, block) {
			continue
		}
		for _, instruction := range block.Instructions {
			call, isCall := instruction.(*intermediate_representation.Call)
			if !isCall {
				continue
			}
			callee := call.Call.StaticCallee()
			if callee == nil {
				continue
			}
			passed := passedParameters(call, callee)
			if len(passed) == 0 {
				// A call that passes no parameter on marks nothing, so what it calls is never read for it.
				continue
			}
			holder := walk.holder(callee)
			for _, each := range passed {
				reached.passes = append(reached.passes, reachedPass{passedParameter: each, callee: holder})
			}
		}
	}
	reached.read = true
	walk.reached = append(walk.reached, reached)
}

// holder is the function whose marks hold the marks of callee's parameters, reached first when its marks are not
// final: callee itself, a function of home or of none, and nil for a function of another package.
func (walk *demandWalk) holder(
	callee *intermediate_representation.Function,
) *intermediate_representation.Function {
	if callee.Package != walk.irPackage && callee.Package != nil {
		return nil
	}
	if _, final := walk.state.functions[callee]; final {
		return callee
	}
	begun, isBegun := walk.byFunction[callee]
	if !isBegun {
		walk.reach(callee)
		return callee
	}
	if !begun.read {
		walk.cyclic = true
	}
	return callee
}

// settle passes the definition over the reached functions until a pass adds no mark, keeps each one's marks, final,
// and returns the functions it settled, leaving the walk with none reached.
func (walk *demandWalk) settle() []*intermediate_representation.Function {
	for added := true; added; {
		added = false
		for _, reached := range walk.reached {
			for _, pass := range reached.passes {
				if reached.marked[pass.parameter.Object()] || !walk.marked(pass) {
					continue
				}
				reached.marked[pass.parameter.Object()] = true
				added = true
			}
		}
		if !walk.cyclic {
			// Each pass read a callee settled before it, so nothing is left for another pass to add.
			break
		}
	}
	settled := make([]*intermediate_representation.Function, 0, len(walk.reached))
	for _, reached := range walk.reached {
		walk.state.functions[reached.function] = reached.marked
		settled = append(settled, reached.function)
		if reached.function.Package != nil {
			walk.resolver.Counts(Kind).Computed.Add(1)
		}
	}
	walk.reached = nil
	walk.cyclic = false
	clear(walk.byFunction)
	return settled
}

// marked is whether the parameter pass passes to is marked, as far as the walk has settled: by its holder's marks,
// final or reached, or, for another package's parameter, by the resolver's answer as home holds it.
func (walk *demandWalk) marked(pass reachedPass) bool {
	if pass.callee == nil {
		return ask(walk.resolver, walk.home, pass.calleeParameter)
	}
	if reached, isReached := walk.byFunction[pass.callee]; isReached {
		return reached.marked[pass.calleeParameter]
	}
	return walk.state.functions[pass.callee][pass.calleeParameter]
}
