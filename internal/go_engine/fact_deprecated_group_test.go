package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// A deprecation can differ between builds with its declaration's text and file the same in each (#7v2bzvd): the
// deprecated facts analyzer carries a deprecation paragraph above an empty `var ()` to the next declaration it meets,
// in that file or the next. In both fixtures the paragraph sits in a platform file of api and Old in its shared file,
// so Old is deprecated in exactly the builds that compile the paragraph's file: the host's in the -host fixture,
// windows's in the -extra one. Target names only Old, so the reach leaves it standing, and on the host's findings it
// would invent windows a deprecated use in the first and lose windows's in the second. Old's deprecation fact is
// compared and names it, so Target leaves for "deprecation" either way, and so does Old's own body in api, whose
// findings read whether Old itself is deprecated. Windows's own findings are the same with the standing bodies' switch
// off and on, and the second leg finds nothing standing that differs.
func TestADeprecationCarriedPastAnEmptyGroupTakesItsUserOut(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		fixture string
		module  string
		// onHost is whether the host's build holds Old's deprecation, and windows's does not; else the reverse.
		onHost bool
	}{
		{"fact-deprecated-group-host", "example.com/deprecatedgrouphost", true},
		{"fact-deprecated-group-extra", "example.com/deprecatedgroupextra", false},
	} {
		test.Run(each.fixture, func(test *testing.T) {
			test.Parallel()

			settled := settleFactFixture(test, each.fixture, each.module+"/reader")
			if reason := settled.bodies["Target"].reason; reason != "" {
				test.Fatalf("the reach reached Target for %q, want it unreached, so that only the facts can take it "+
					"out", reason)
			}
			settled.requireRemovedBy(test, "Target", "Old", "deprecation")
			// Old's own body leaves too, for SA1019 reads the deprecation of the function a use sits in.
			api, isSettled := settled.run.result.FactReachSettled[each.module+"/api"].(*standing_facts.Result)
			if !isSettled || api.Settlement == nil {
				test.Fatalf("windows settled api as %+v, want it settled in part", api)
			}
			ownRemoved := slices.ContainsFunc(api.Settlement.Removed, func(removal fact_reach.Removal) bool {
				return strings.HasSuffix(removal.Place.File, "shared.go") && removal.Reason == "deprecation" &&
					slices.Contains(strings.Split(string(removal.Object), "\x00"), "Old")
			})
			if !ownRemoved {
				test.Errorf("api's settlement removed %v, want Old's own body out for its deprecation",
					api.Settlement.Removed)
			}
			deprecatedUse := func(findings []check.Finding) bool {
				return slices.ContainsFunc(findings, func(finding check.Finding) bool {
					return finding.Rule == "cohere-go/SA1019" && strings.HasSuffix(finding.File, "reader.go")
				})
			}
			if deprecatedUse(settled.host.result.Findings) != each.onHost ||
				deprecatedUse(settled.run.result.Findings) == each.onHost {
				test.Errorf("Target's use of Old is a deprecated use on the host %t and on windows %t, want the "+
					"host's %t and windows's the other", deprecatedUse(settled.host.result.Findings),
					deprecatedUse(settled.run.result.Findings), each.onHost)
			}
			differential := settled.run.bodyDifferential
			if differential == nil {
				test.Fatal("windows's build ran no body differential")
			}
			if len(differential.standingFailed) > 0 {
				test.Errorf("windows's second leg failed %v, among the differential's failures\n%s\nwant nothing "+
					"standing that differs", differential.standingFailed, strings.Join(differential.failures, "\n"))
			}

			var own [2][]string
			for index, switched := range []bool{false, true} {
				root := copyFixture(test, filepath.Join("testdata", each.fixture))
				engine, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
					engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
					engine.standingBodies = switched
				}, "--no-cache", "--no-fix", "--build-dependence")
				if len(engine.buildRuns) != 2 {
					test.Fatalf("want the host's build and windows's, got %d runs", len(engine.buildRuns))
				}
				own[index] = rawFindings(engine.buildRuns[1], root)
			}
			if !slices.Equal(own[0], own[1]) {
				test.Errorf("windows's own findings with the standing bodies' switch on are\n%s\nwant those with it "+
					"off\n%s", strings.Join(own[1], "\n"), strings.Join(own[0], "\n"))
			}
		})
	}
}
