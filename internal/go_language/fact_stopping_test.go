package go_language

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_language/fact_reach"
)

// A kind that does not record every judgment keeps the unrecorded rule whole (#5vbs1np, must 1, and #z9fze0t): a body
// the build lints with no record of it reads unknown for it, since a judgment may have read the body and left no
// record. In the fact-stopping fixture the host answered the stopping calls about the package, and purity, which
// records every judgment, and no other kind. The reach lints helper, whose call selects a testing.T's method on every
// platform but windows, and no body asks helper's stopping calls, which stopVia's judgment walks into, so helper holds
// no record of the kind and takes stopVia, its one user, out for that. Left out of the kinds a body must hold a record
// of, the stopping calls would name nothing there, and stopVia would stand on the host's findings.
func TestABodyTheBuildLintsWithNoRecordOfTheStoppingCallsTakesItsUserOut(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-stopping", "example.com/stopping")
	if settled.bodies["helper"].reason == "" {
		test.Fatal("the reach left helper unreached, want it reached, so that the build asks its records")
	}
	if reason := settled.bodies["stopVia"].reason; reason != "" {
		test.Fatalf("the reach reached stopVia for %q, want it unreached, so that only the facts can take it out",
			reason)
	}
	settled.requireRemovedBy(test, "stopVia", "helper", fact_reach.UnrecordedReason("test_stopping_calls"))
}
