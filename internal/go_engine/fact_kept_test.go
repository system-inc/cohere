package go_engine

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// A callee a walk judged inside its caller's judgment has a record of its own (#z9fze0t): in the fact-kept fixture no
// body asks inner's purity, which outer's judgment judged, and the reach lints inner, which calls what each platform's
// file declares. The walk handed the resolver inner's verdict, so the host holds its record, the build compares it,
// and it is the same on windows: outer, which names only inner, stands, and so does Twice, which discards outer's
// result. Without the record inner read unknown, and took outer out as holding no record of the kind. inner holds a
// record of the other kinds the build can ask too, so purity's is the one this turns on. Where a kept answer differs
// its user still leaves, as size does for helper's in the fact-kinds fixture
// (TestTheFactsReachTakesOutWarmWhoseCalleesPurityDiffers). Nothing stands that the second leg could find differing.
func TestACalleeWhoseKeptPurityIsTheSameLeavesItsUserStanding(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-kept", "example.com/kept")
	if settled.bodies["inner"].reason == "" {
		test.Fatal("the reach left inner unreached, want it reached, so that the build compares its record")
	}
	for _, name := range []string{"outer", "Twice"} {
		body, isListed := settled.bodies[name]
		if !isListed {
			test.Fatalf("the census lists no body %s", name)
		}
		if body.reason != "" {
			test.Fatalf("the reach reached %s for %q, want it unreached, so that only the facts can take it out", name,
				body.reason)
		}
		if !settled.settlement.Standing[fact_reach.Place{File: body.file, Offset: body.offset}] {
			removal, _ := settled.removal(test, name)
			test.Errorf("%s does not stand, taken out by %q for %q, want it standing", name, removal.Object,
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
