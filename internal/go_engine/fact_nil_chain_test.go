package go_engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// Nil-ness's kept answers are what take a chain's users out, through each of its two walks (#z9fze0t). In the
// fact-nil-chain fixture the reach lints leafShared and leafReplayed, which call what each platform's file declares,
// and nothing else: no body asks either leaf's nil-ness or a middle's, which top's judgment judges inside its own. The
// demand walk hands the resolver leafShared's verdict and middle's, and the replay leafReplayed's and middleReplayed's,
// since middleReplayed's cycle leaves the demand walk's answer to the push's order. Each leaf's error is never nil on
// windows alone, so its record differs there and takes its middle out, whose own record then differs and takes its top
// out, whose differs and takes its User out: windows's SA4023 in each User is the build's own.
//
// Nil-ness records every judgment, so nothing is named for holding no record of it, and the host answered no other
// kind about the package. Were either walk to hand nothing over, its leaf would hold no answer, its middle would stand
// on the host's findings, and so would its User, with windows's finding lost, which the second leg holds too.
func TestNilnessKeptAnswersTakeEachChainsUserOutThroughBothWalks(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-nil-chain", "example.com/nilchain")
	for _, name := range []string{"leafShared", "leafReplayed"} {
		if settled.bodies[name].reason == "" {
			test.Fatalf("the reach left %s unreached, want it reached, so that the build compares its record", name)
		}
	}
	for _, name := range []string{"middle", "top", "User", "middleReplayed", "topReplayed", "UserReplayed"} {
		body, isListed := settled.bodies[name]
		if !isListed {
			test.Fatalf("the census lists no body %s", name)
		}
		if body.reason != "" {
			test.Fatalf("the reach reached %s for %q, want it unreached, so that only the facts can take it out", name,
				body.reason)
		}
	}
	nilness := fact_reach.DiffersReason("return_nil_ness")
	for _, chain := range [][]string{
		{"leafShared", "middle", "top", "User"},
		{"leafReplayed", "middleReplayed", "topReplayed", "UserReplayed"},
	} {
		for index := 1; index < len(chain); index++ {
			settled.requireRemovedBy(test, chain[index], chain[index-1], nilness)
		}
	}
	if len(settled.settlement.Unrecorded) != 0 {
		test.Errorf("functions were named for holding no record of a kind, %v, want none",
			settled.settlement.Unrecorded)
	}
	compares := func(findings []check.Finding, user string) bool {
		return slices.ContainsFunc(findings, func(finding check.Finding) bool {
			return finding.Rule == "cohere-go/SA4023" && finding.Line == settled.bodies[user].line+1
		})
	}
	for _, user := range []string{"User", "UserReplayed"} {
		if !compares(settled.run.result.Findings, user) || compares(settled.host.result.Findings, user) {
			test.Errorf("%s's comparison is a finding on windows %t and on the host %t, want windows's alone", user,
				compares(settled.run.result.Findings, user), compares(settled.host.result.Findings, user))
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
