package return_nil_ness

import (
	"cmp"
	"go/types"
	"maps"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// OrderBound is every function of home a read of which may answer otherwise in a walk of home in another order, as
// far as this run's walks of home saw, sorted by position and then name, and whether the kind's state can say so. It
// is not known when a function of held, the functions of home the caller holds a nilness answer about, has no verdict
// here, as one answered from the store or by a test's hand has none, and then the caller takes the package whole.
//
// The push reads a callee in one order with one seen set, so a read hangs on the order only where its callee can be
// unfinished when it is read (#h0k644s). One such read is a call into another member of the reader's group, its
// cycle of calls, since which member the walk enters first decides which call back answers the defaults. A call of a
// function to itself is not one: the function is begun whenever its own body is judged, in any order. The other is a
// call through a stand-in, an instantiation or a method wrapper of a declared function of home, whose fact the walk
// holds only once it has judged the declared function, and the defaults before. A callee whose verdict ends at its
// floor, no more than the defaults of its signature, answers the defaults finished or not, so when every target of
// such reads ends at its floor every answer is the same in every order. So a group is bound, every member, when any
// member says more than the defaults, and a function read through a stand-in when it does, or when no walk judged it,
// so its final verdict is not known.
//
// The bound is held in process only: nothing of it goes into an answer or the store.
func OrderBound(home *fact_resolver.Package, held []*types.Func) (bound []*types.Func, known bool) {
	home.Lock()
	defer home.Unlock()
	state := stateIn(home)
	for _, function := range held {
		if _, judged := state.results[function]; !judged {
			return nil, false
		}
	}
	bounded := map[*types.Func]bool{}
	for _, group := range groupsOf(state.callees) {
		if slices.ContainsFunc(group, state.aboveFloor) {
			for _, member := range group {
				bounded[member] = true
			}
		}
	}
	for standIn := range state.standIns {
		if state.aboveFloor(standIn) {
			bounded[standIn] = true
		}
	}
	return slices.SortedFunc(maps.Keys(bounded), func(left *types.Func, right *types.Func) int {
		return cmp.Or(cmp.Compare(left.Pos(), right.Pos()), cmp.Compare(left.FullName(), right.FullName()))
	}), true
}

// aboveFloor is whether function's verdict says more than the defaults of its signature, or is not known, since no
// walk has judged it. The caller holds home's lock.
func (state *verdicts) aboveFloor(function *types.Func) bool {
	results, judged := state.results[function]
	return !judged || interesting(function.Signature(), results)
}

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
