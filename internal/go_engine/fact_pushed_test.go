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
