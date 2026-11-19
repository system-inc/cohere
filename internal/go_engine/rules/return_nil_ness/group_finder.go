package return_nil_ness

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// groupsOf is every group of callees, each judged function to the functions of its package its judgment called: each
// strongly connected component of two functions or more, by Tarjan's algorithm.
func groupsOf(callees map[*types.Func][]*intermediate_representation.Function) [][]*types.Func {
	finder := &groupFinder{callees: callees, index: map[*types.Func]int{}, low: map[*types.Func]int{},
		onStack: map[*types.Func]bool{}}
	for function := range callees {
		if _, visited := finder.index[function]; !visited {
			finder.visit(function)
		}
	}
	return finder.groups
}

// groupFinder is Tarjan's search over callees: each function's index in the order the search reached it, the lowest
// index it reaches back to, the functions reached whose component has not closed, and the groups closed.
type groupFinder struct {
	callees map[*types.Func][]*intermediate_representation.Function
	index   map[*types.Func]int
	low     map[*types.Func]int
	onStack map[*types.Func]bool
	stack   []*types.Func
	groups  [][]*types.Func
}

// visit searches from function, closing its component when nothing it reaches reaches back above it.
func (finder *groupFinder) visit(function *types.Func) {
	finder.index[function] = len(finder.index)
	finder.low[function] = finder.index[function]
	finder.stack = append(finder.stack, function)
	finder.onStack[function] = true
	for _, callee := range finder.callees[function] {
		declared := declaredFunction(callee)
		if _, visited := finder.index[declared]; !visited {
			finder.visit(declared)
			finder.low[function] = min(finder.low[function], finder.low[declared])
		} else if finder.onStack[declared] {
			finder.low[function] = min(finder.low[function], finder.index[declared])
		}
	}
	if finder.low[function] != finder.index[function] {
		return
	}
	var component []*types.Func
	for {
		member := finder.stack[len(finder.stack)-1]
		finder.stack = finder.stack[:len(finder.stack)-1]
		delete(finder.onStack, member)
		component = append(component, member)
		if member == function {
			break
		}
	}
	// A function alone, though it calls itself, is no group: it is begun whenever its body is judged, in any order.
	if len(component) > 1 {
		finder.groups = append(finder.groups, component)
	}
}
