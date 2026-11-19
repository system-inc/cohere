package go_engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
)

// Every fact type a run pushes is compared, not only no-return (#5vbs1np, must 2): q's W wraps fmt.Printf on every
// platform but windows, so its wrapper fact, which printf pushes to p, differs on windows, while W's signature and
// no-return fact do not. B, which hands W a string for %d, names only W, so the reach leaves it standing, and on the
// host's findings it would keep a printf finding windows does not have. The wrapper fact names W, and B leaves.
func TestAPrintfWrapperFactThatDiffersTakesItsUserOut(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-pushed", "example.com/pushed/p")
	if settled.bodies["B"].reason != "" {
		test.Fatalf("the reach reached B for %q, want it unreached, so that only the facts can take it out",
			settled.bodies["B"].reason)
	}
	settled.requireRemovedBy(test, "B", "W", "printf wrapper")
	printf := func(findings []check.Finding) bool {
		return slices.ContainsFunc(findings, func(finding check.Finding) bool {
			return finding.Rule == "cohere-go/printf" && strings.HasSuffix(finding.File, "p.go")
		})
	}
	if printf(settled.run.result.Findings) || !printf(settled.host.result.Findings) {
		test.Errorf("B's call is a printf finding on windows %t and on the host %t, want the host's alone",
			printf(settled.run.result.Findings), printf(settled.host.result.Findings))
	}
}

// The deprecation facts are pushed and never compared by the facts' reach, by its written argument (standing_facts'
// pushed facts): a deprecation is read from its declaration's doc comment, so it differs between two builds only where
// the declaration's text does or it sits in a file one build alone compiles, and the reach at body grain takes out
// every user of such a declaration (#5vbs1np). q's Old is deprecated in the file every platform but windows compiles,
// and current in windows's own, so windows's reach lints Caller, which calls it, before any fact is compared, and the
// deprecated use is the host's finding alone: Caller's findings on windows are the build's own.
func TestADeprecationThatDiffersIsTakenOutByTheReach(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-deprecated", "example.com/deprecated/p")
	if settled.bodies["Caller"].reason == "" {
		test.Fatalf("the reach left Caller unreached, so only the facts could take it out, and no fact does: a hole " +
			"in the deprecation's argument")
	}
	if removal, removed := settled.removal(test, "Caller"); removed {
		test.Errorf("the facts' reach took Caller out, by %q for %q, which the reach had taken already",
			removal.Object, removal.Reason)
	}
	deprecated := func(findings []check.Finding) bool {
		return slices.ContainsFunc(findings, func(finding check.Finding) bool {
			return finding.Rule == "cohere-go/SA1019" && strings.HasSuffix(finding.File, "p.go")
		})
	}
	if deprecated(settled.run.result.Findings) || !deprecated(settled.host.result.Findings) {
		test.Errorf("Caller's use of Old is a deprecated use on windows %t and on the host %t, want the host's alone",
			deprecated(settled.run.result.Findings), deprecated(settled.host.result.Findings))
	}
}
