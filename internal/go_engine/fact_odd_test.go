package go_engine

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// A kind that answers about each parameter is recorded by the parameter (#5vbs1np, round 2's N1): G, which the reach
// lints, holds odd slice's record of its first parameter, which GUser asked, and none of its second, whose mark F's
// own was judged from in the kind's state, and which needs an even slice on windows alone. So G's second reads unknown
// and takes F out, though G holds a record of the kind; F's own mark, which FUser asked, then differs, and takes FUser
// out, whose odd slice is windows's finding alone.
func TestAParameterWithNoRecordOfItsKindReadsUnknown(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-odd", "example.com/odd")
	for _, name := range []string{"F", "FUser"} {
		if settled.bodies[name].reason != "" {
			test.Fatalf("the reach reached %s for %q, want it unreached, so that only the facts can take it out", name,
				settled.bodies[name].reason)
		}
	}
	settled.requireRemovedBy(test, "F", "G", fact_reach.UnrecordedReason("odd_slice_argument"))
	settled.requireRemovedBy(test, "FUser", "F", fact_reach.DiffersReason("odd_slice_argument"))
}
