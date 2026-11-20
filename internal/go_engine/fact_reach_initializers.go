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
// settled with nothing standing (anyAmong), where a rule's standing bodies are none and every rule hears every body
// (buildStanding.bodiesOf), so the build lints all of it, and the second leg compares nothing (compareStanding). The
// initializers leave by the objects a settlement named instead (leaveFor), and only the body differential, which
// compares them, and the census read that (takenOutByFacts).
type factReachInitializers struct {
	places     []fact_reach.Place
	uses       map[fact_reach.Object]bool
	everything bool
}

// anyAmong is whether any of the package's variable initializers is among unreached, the places a build's reach
// leaves unreached, so that the build has the package's settlement to read whether they leave (factReachSides.inBuild).
func (initializers factReachInitializers) anyAmong(unreached map[fact_reach.Place]bool) bool {
	return slices.ContainsFunc(initializers.places, func(place fact_reach.Place) bool { return unreached[place] })
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
