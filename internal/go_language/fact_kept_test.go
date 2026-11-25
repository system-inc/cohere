package go_language

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/fact_reach"
)

// A callee a walk judged inside its caller's judgment has a record of its own, which the build compares (#z9fze0t). In
// the fact-kept fixture no body asks inner's purity or shifting's, each judged by its caller's judgment, and the reach
// lints both, which call what each platform's file declares. inner's kept answer is the same on windows, so outer,
// which names only inner, stands, and so does Twice, which discards outer's result. shifting's differs on windows, so
// outerShifting leaves for it, and TwiceShifting for outerShifting's own, whose finding is windows's alone.
//
// Purity records every judgment, so no function is named for holding no record of it: the kept records are what tell
// the two chains apart. With the walk handing nothing over, shifting would hold no answer, outerShifting would stand
// on the host's findings, and windows's finding in TwiceShifting would be lost, which the second leg holds too.
func TestACalleesKeptPurityLeavesItsUserStandingWhereItIsTheSameAndTakesItOutWhereItDiffers(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-kept", "example.com/kept")
	for _, name := range []string{"inner", "shifting"} {
		if settled.bodies[name].reason == "" {
			test.Fatalf("the reach left %s unreached, want it reached, so that the build compares its record", name)
		}
	}
	for _, name := range []string{"outer", "Twice", "outerShifting", "TwiceShifting"} {
		body, isListed := settled.bodies[name]
		if !isListed {
			test.Fatalf("the census lists no body %s", name)
		}
		if body.reason != "" {
			test.Fatalf("the reach reached %s for %q, want it unreached, so that only the facts can take it out", name,
				body.reason)
		}
	}
	for _, name := range []string{"outer", "Twice"} {
		body := settled.bodies[name]
		if !settled.settlement.Standing[fact_reach.Place{File: body.file, Offset: body.offset}] {
			removal, _ := settled.removal(test, name)
			test.Errorf("%s does not stand, taken out by %q for %q, want it standing", name, removal.Object,
				removal.Reason)
		}
	}
	settled.requireRemovedBy(test, "outerShifting", "shifting", fact_reach.DiffersReason("function_purity"))
	settled.requireRemovedBy(test, "TwiceShifting", "outerShifting", fact_reach.DiffersReason("function_purity"))
	if unrecorded := settled.settlement.Unrecorded["function_purity"]; unrecorded != 0 {
		test.Errorf("%d functions were named for holding no record of purity, which records every judgment, want none",
			unrecorded)
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
