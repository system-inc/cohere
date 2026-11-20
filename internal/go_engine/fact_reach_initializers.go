package go_engine

import (
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// factReachInitializers is a package's variable initializers, each value spec of a var with a value, as one asker of
// the facts' reach (#bed0mnk, the Square's must 1). Go lowers them into one synthetic function, cut after a call that
// never returns, so a fact or an answer that differs about a function any one of them uses can move the findings of
// every one. places are where each is declared, as the census places a body; uses are the keys of the objects any of
// them uses; and everything is whether one holds a use the record could not follow, which uses every object.
//
// The asker never stands. No initializer is among the side's places, its function bodies, which a build's standing set
// is taken from (factReachSides.inBuild), and a settlement's is a copy of that set which only loses bodies
// (fact_reach.Settle). So no reader of a standing set meets one: a package with nothing but initializers unreached is
// linted whole, a rule's standing bodies hold none (buildStanding.bodiesOf), and the second leg compares none
// (compareStanding). The initializers leave by the objects a settlement named instead (leaveFor), and only the body
// differential, which compares them, and the census read that (takenOutByFacts).
type factReachInitializers struct {
	places     []fact_reach.Place
	uses       map[fact_reach.Object]bool
	everything bool
}

// leaveFor is whether the package's variable initializers leave together for named, the objects a settlement of the
// package named (fact_reach.Settlement.Named): one of them uses a named object, or holds a use the record could not
// follow while anything at all is named, as a body that uses every object leaves.
func (initializers factReachInitializers) leaveFor(named []fact_reach.Named) bool {
	if len(initializers.places) == 0 {
		return false
	}
	if initializers.everything {
		return len(named) > 0
	}
	return slices.ContainsFunc(named, func(each fact_reach.Named) bool { return initializers.uses[each.Object] })
}
