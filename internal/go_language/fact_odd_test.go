package go_language

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/fact_reach"
	"github.com/system-inc/cohere/internal/go_language/rules/standing_presence"
)

// A kind that answers about each parameter is recorded by the parameter (#5vbs1np, round 2's N1), and a judgment hands
// the resolver every mark it settles, so each is on record beside the asked one (#z9fze0t): G, which the reach lints,
// holds odd slice's record of its first parameter, which GUser asked, and of its second, which no body asked and F's
// own mark was judged from inside the kind's state. G's second needs an even slice on windows alone, so its record
// differs there and takes F out; F's own mark, which FUser asked, then differs, and takes FUser out, whose odd slice
// is windows's finding alone.
func TestAParameterNoBodyAskedOfIsOnRecordAndTakesItsUserOutWhereItDiffers(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-odd", "example.com/odd")
	for _, name := range []string{"F", "FUser"} {
		if settled.bodies[name].reason != "" {
			test.Fatalf("the reach reached %s for %q, want it unreached, so that only the facts can take it out", name,
				settled.bodies[name].reason)
		}
	}
	settled.requireRemovedBy(test, "F", "G", fact_reach.DiffersReason("odd_slice_argument"))
	settled.requireRemovedBy(test, "FUser", "F", fact_reach.DiffersReason("odd_slice_argument"))
}

// The odd slice shape written with any (#xeafx79, the reader's S2), which the facts fixtures write by an alias: Pairs's
// parameter is a key of consistency-no-any's, so presence's bound names Pairs, and PairsUser, which the reach leaves
// standing, leaves for the bound, before the facts would take it out for Pairs's odd slice answer. Plain stands.
func TestABodyNamingAFunctionWithAnAnyParameterLeavesForTheBound(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-any", "example.com/anything")
	for _, name := range []string{"PairsUser", "Plain"} {
		if settled.bodies[name].reason != "" {
			test.Fatalf("the reach reached %s for %q, want it unreached, so that only presence or the facts can take "+
				"it out", name, settled.bodies[name].reason)
		}
	}
	settled.requireRemovedBy(test, "PairsUser", "Pairs", standing_presence.NoAny.Reason())
	if removal, removed := settled.removal(test, "Plain"); removed {
		test.Errorf("Plain left the standing set, by %q for %q, want it standing", removal.Object, removal.Reason)
	}
}

// A callee whose every parameter's mark is on record, and the same in the build, takes no user out (#z9fze0t): H is G
// with a second parameter no build needs even. The reach lints H, which calls what each platform's file declares, and
// H holds a record of every kind: its purity and its nil-ness, which HUser and HFailed asked, its first parameter's
// mark, which HUser asked, and its second's, which no body asked and the judgment of its first settled and handed
// over. Each reads the same on windows, so nothing names H, and its users stand: E, which hands its slice on as H's
// second, EUser, HUser and HFailed. Before a judgment's settled marks were on record H's second read unknown, and took
// all four out as G's took F.
func TestACalleeWhoseEveryParametersMarkIsOnRecordLeavesItsUsersStanding(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-odd", "example.com/odd")
	if settled.bodies["H"].reason == "" {
		test.Fatal("the reach left H unreached, want it reached, so that the build compares its records")
	}
	for _, name := range []string{"E", "EUser", "HUser", "HFailed"} {
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
