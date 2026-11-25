package go_language

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/fact_reach"
)

// The fact-types-alone fixture's initializer calls its `init`, so the host holds purity's answer about a function no
// identity names, which no place can take (#30dtg3p). Purity marks that answer as its types' alone, not pure in every
// build, so the host's side leaves it out, counted nowhere, and windows's build settles the package in part: init's
// body, double's and Twice's, which the reach leaves unreached, stand on the host's findings, where one answer counted
// unplaced had every build lint the package whole. Name, which calls what each platform's file declares, is the
// build's to lint.
//
// The second leg fails nothing, which is said of double and Twice and of nothing else. The leg compares the functions
// the host's side declares at each standing place (factReachPackage.declared), and a body no identity names, as an
// `init` function's, has none, so the leg compares nothing of init, and its failures are empty for init by
// construction. What holds init's answer to not pure in every build is TestEveryFunctionTheHookNamesIsJudgedNotPure,
// function_purity's own, and what keeps an `init` from standing where a callee's answer differs is
// TestAnInitWhoseCalleesPurityDiffersNeverStands.
func TestAnInitsPurityLeavesItsPackagesUnreachedBodiesStanding(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-types-alone", "example.com/alone")
	for _, name := range []string{"init", "double", "Twice"} {
		body, isListed := settled.bodies[name]
		if !isListed {
			test.Fatalf("the census lists no body %s", name)
		}
		if body.reason != "" {
			test.Fatalf("the reach reached %s for %q, want it unreached, so that it stands or the package lints whole",
				name, body.reason)
		}
		if !settled.settlement.Standing[fact_reach.Place{File: body.file, Offset: body.offset}] {
			removal, _ := settled.removal(test, name)
			test.Errorf("%s does not stand, taken out by %q for %q, want it standing", name, removal.Object,
				removal.Reason)
		}
	}
	if settled.bodies["Name"].reason == "" {
		test.Error("the reach left Name unreached, want it reached, so that the build differs somewhere")
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
