package return_nil_ness

import (
	"cmp"
	"go/types"
	"maps"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/call_groups"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
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
	// Each judged function to the declared functions of home its judgment called. One that called none is in no group.
	callees := make(map[*types.Func][]*types.Func, len(state.callees))
	for function, called := range state.callees {
		for _, callee := range called {
			callees[function] = append(callees[function], declaredFunction(callee))
		}
	}
	bounded := map[*types.Func]bool{}
	for _, group := range call_groups.Of(callees) {
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
