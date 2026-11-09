package odd_slice_argument

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// demandWalk finds the marks of one function of home and of what it calls there, depth first, under home's lock.
//
// The push marks every source function's own checks first, then visits the functions in one order with one seen set,
// marking a parameter passed on to a callee's marked parameter, the callee visited first when it is of the same package
// or of none. A callee already begun has marks only as far as its visit has come, and which callee is still begun when
// a call reaches it depends on where the push entered the cycle, so there the answer depends on the push's order: the
// walk stops, and ask marks the whole package the push's way. A function calling itself is not such a call: it is
// begun whenever its own calls are read, in any order, so it reads its own marks as far as they have come, as here.
//
// One more read depends on the order: a function of no package, an instantiation or a wrapper, calling one of home's
// without visiting it, as the push visits only a callee of the caller's own package or of none. It reads that one's
// marks as far as the push had come, which is all of its own checks but only those of its calls the push had visited
// by then. A mark from its own checks is read as it is; a mark from its calls makes the walk stop the same way.
type demandWalk struct {
	resolver  *fact_resolver.Resolver
	home      *fact_resolver.Package
	irPackage *intermediate_representation.Package
	state     *marks
	stack     []*intermediate_representation.Function

	// partial is the marks of each function on the stack, as far as its walk has come.
	partial map[*intermediate_representation.Function]map[types.Object]bool

	// orderDependent is whether the walk met a read whose answer depends on the push's order, after which its marks
	// are no answer, and it ends as soon as it can.
	orderDependent bool
}

// verdict is the marks of function, a source function of home or a function of no package, as the push leaves them
// once it has visited function: its own checks, and each parameter it passes on to a marked one.
func (walk *demandWalk) verdict(function *intermediate_representation.Function) map[types.Object]bool {
	if marked, judged := walk.state.functions[function]; judged {
		return marked
	}
	if partial, begun := walk.partial[function]; begun {
		walk.orderDependent = walk.orderDependent || function != walk.stack[len(walk.stack)-1]
		return partial
	}
	function.Build()
	marked := map[types.Object]bool{}
	if function.Package != nil {
		for _, parameter := range directChecks(function) {
			marked[parameter.Object()] = true
		}
	}
	walk.stack = append(walk.stack, function)
	walk.partial[function] = marked
	for _, block := range function.Blocks {
		if walk.orderDependent {
			break
		}
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
			visited := callee.Package == function.Package || callee.Package == nil
			if visited {
				walk.verdict(callee)
			}
			for _, passed := range passedParameters(call, callee) {
				if walk.marked(callee, visited, passed.calleeParameter) {
					marked[passed.parameter.Object()] = true
				}
			}
		}
	}
	walk.stack = walk.stack[:len(walk.stack)-1]
	delete(walk.partial, function)
	if walk.orderDependent {
		// Found after the walk stopped, so not found at all; the fallback marks it.
		return marked
	}
	if walk.state.functions == nil {
		walk.state.functions = map[*intermediate_representation.Function]map[types.Object]bool{}
	}
	walk.state.functions[function] = marked
	if function.Package != nil {
		walk.resolver.Counts(Kind).Computed.Add(1)
	}
	return marked
}

// marked is whether the push holds parameter, of callee, marked when a call to callee reads it: callee's marks once
// visited, when visited says the push visited it first; a mark asked of the resolver for another package's parameter;
// or, for one of home's functions the push did not visit first, its own checks (see demandWalk).
func (walk *demandWalk) marked(
	callee *intermediate_representation.Function,
	visited bool,
	parameter *types.Var,
) bool {
	if visited {
		return walk.verdict(callee)[parameter]
	}
	if callee.Package != walk.irPackage {
		return ask(walk.resolver, walk.home, parameter)
	}
	callee.Build()
	for _, checked := range directChecks(callee) {
		if checked.Object() == types.Object(parameter) {
			return true
		}
	}
	if walk.verdict(callee)[parameter] {
		walk.orderDependent = true
	}
	return false
}
