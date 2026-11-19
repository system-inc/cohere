package go_engine

import (
	"strings"
	"testing"
)

// A cycle of calls that forms in the build alone (#5vbs1np, must 3): on windows platformHook calls orderA back, closing
// orderA, orderB, platformHook, so windows's answer about orderA hangs on which member its walk enters first, while
// the host's walk, with no cycle, bounds no order. orderB, which the reach lints, answers every kind as the host did,
// so only the build's own groups name the cycle's members: orderA leaves as orderB's user, and OrderUser and
// OrderDiscard as orderA's and orderB's. stillHead and stillTail call each other in every build and stand, so their
// cycle names nothing, and StillUser stands. Nothing stands that the second leg could find differing.
func TestACycleThatFormsInTheBuildAloneTakesItsMembersUsersOut(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-order", "example.com/order")
	for _, name := range []string{"orderA", "OrderUser", "OrderDiscard"} {
		if settled.bodies[name].reason != "" {
			test.Fatalf("the reach reached %s for %q, want it unreached, so that only the facts can take it out", name,
				settled.bodies[name].reason)
		}
	}
	settled.requireRemovedBy(test, "orderA", "orderB", "build order")
	settled.requireRemovedBy(test, "OrderUser", "orderA", "build order")
	settled.requireRemovedBy(test, "OrderDiscard", "orderB", "build order")
	for _, name := range []string{"stillHead", "stillTail", "StillUser"} {
		if removal, removed := settled.removal(test, name); removed {
			test.Errorf("%s left the standing set, by %q for %q, want it standing", name, removal.Object,
				removal.Reason)
		}
	}
	differential := settled.run.bodyDifferential
	if differential == nil {
		test.Fatal("windows's build ran no body differential")
	}
	if len(differential.standingFailed) > 0 {
		test.Errorf("windows's second leg failed %v, among the differential's failures\n%s\nwant nothing standing "+
			"that differs", differential.standingFailed, strings.Join(differential.failures, "\n"))
	}
}
