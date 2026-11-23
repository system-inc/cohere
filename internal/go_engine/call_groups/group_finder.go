// Package call_groups finds the groups of a package's functions that call each other, each a cycle of calls, for the
// two readers that ask which answers hang on the order a walk enters a cycle in (#h0k644s): nil-ness's order bound
// over the callees its walks recorded, and the facts' reach over the calls a build's own syntax makes (#5vbs1np).
package call_groups

import "github.com/system-inc/go-types/types"

// Of is every group of callees, each function to the functions it calls: each strongly connected component of two
// functions or more, by Tarjan's algorithm, in no order. A function alone, though it calls itself, is no group: it is
// begun whenever its own body is judged, in any order.
func Of(callees map[*types.Func][]*types.Func) [][]*types.Func {
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
	callees map[*types.Func][]*types.Func
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
		if _, visited := finder.index[callee]; !visited {
			finder.visit(callee)
			finder.low[function] = min(finder.low[function], finder.low[callee])
		} else if finder.onStack[callee] {
			finder.low[function] = min(finder.low[function], finder.index[callee])
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
	if len(component) > 1 {
		finder.groups = append(finder.groups, component)
	}
}
